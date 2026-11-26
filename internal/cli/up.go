package cli

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"flowsint-desk/assets"
	"flowsint-desk/internal/config"
	"flowsint-desk/internal/dock"
	"flowsint-desk/internal/lock"
	"flowsint-desk/internal/run"
	"flowsint-desk/internal/secrets"
	"flowsint-desk/internal/session"
	"flowsint-desk/internal/snap"
	"flowsint-desk/internal/sockproxy"
	"flowsint-desk/internal/store"

	"github.com/spf13/cobra"
)

var upSnapshot string

var upCmd = &cobra.Command{
	Use:   "up",
	Short: "Desbloquea el almacén, restaura el último snapshot y levanta Flowsint (Ctrl+C para guardar y cerrar)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		// 1. directorio de ejecución en tmpfs
		runDir, err := run.Ensure()
		if err != nil {
			return err
		}
		if old, err := session.Read(runDir); err == nil && old.Alive() {
			if !old.Ready() {
				return fmt.Errorf("ya hay un `up` arrancando (pid %d); espera a que termine o usa `flowsint-desk down`", old.PID)
			}
			return fmt.Errorf("ya hay una sesión viva (pid %d, puerto %d); usa `flowsint-desk down`", old.PID, old.Port)
		}
		// Señales desde YA: sin esto, un Ctrl+C a mitad del arranque mataba el
		// proceso dejando el daemon, los contenedores y los datos restaurados
		// vivos en RAM, y un `backup` (SIGUSR1) lo mataba directamente.
		sigc := make(chan os.Signal, 8)
		signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1)
		defer signal.Stop(sigc)
		host, _ := os.Hostname()
		boot, _ := run.BootID()
		// 1b. limpieza de restos de un kill -9 / corte (ANTES de comprobar el puerto,
		//     porque un daemon huérfano puede tener el puerto ocupado):
		//     - todos los /tmp/flowsint-desk-*.stale: matar su daemon y sus contenedores
		//     - el run dir actual si tiene session.json muerta o socket vivo
		var staleDirs []string
		var redactVals []string // valores a enmascarar (***) en avisos y logs
		// daemons huérfanos cuyo socket ya no es alcanzable (dir renombrado):
		if k := dock.KillOrphanDaemons("/tmp/flowsint-desk-"); len(k) > 0 {
			fmt.Printf("· daemons rootless huérfanos eliminados (pids %v)\n", k)
		}
		limpiarDir := func(dir string) {
			envO := dock.Env(dir)
			if dock.Alive(envO) {
				for _, c := range []string{"flowsint-desk-app", "flowsint-desk-celery", "flowsint-desk-api", "flowsint-desk-neo4j", "flowsint-desk-redis", "flowsint-desk-postgres"} {
					_, _ = dock.Run(envO, "docker", "rm", "-f", c)
				}
			}
			dock.StopDaemon(dir)
		}
		if g, _ := filepath.Glob("/tmp/flowsint-desk-*.stale"); len(g) > 0 {
			var propios []string
			for _, s := range g {
				// /tmp es de todos: solo se tocan restos que sean un directorio
				// real y nuestro (un .stale ajeno traería su propio docker.sock
				// y un dockerd.pid apuntando a cualquier proceso nuestro)
				if run.Private(s) == nil {
					propios = append(propios, s)
				}
			}
			if len(propios) > 0 {
				fmt.Println("· detectados restos (.stale) de sesiones anteriores: se limpian")
			}
			for _, s := range propios {
				limpiarDir(s)
				staleDirs = append(staleDirs, s)
			}
		}
		_, statErr := os.Stat(run.Socket(runDir))
		_, sessErr := session.Read(runDir)
		if statErr == nil || sessErr == nil {
			fmt.Println("· detectados restos de una sesión anterior (kill -9 / corte): se limpian")
			limpiarDir(runDir)
			stale := staleName(runDir)
			if err := os.Rename(runDir, stale); err != nil {
				return fmt.Errorf("no se pudieron apartar los restos de la sesión anterior (%s): %w", runDir, err)
			}
			staleDirs = append(staleDirs, stale)
			runDir, err = run.Ensure()
			if err != nil {
				return err
			}
		}
		// 1c. registrar la sesión desde el principio (fase "arrancando"): un
		//     segundo `up` se niega en vez de matar a este como si fuera un resto,
		//     y `down` puede cancelar el arranque.
		sess := &session.Session{PID: os.Getpid(), Port: cfg.Port, Started: time.Now().UTC().Format(time.RFC3339),
			Host: host, BootID: boot, Phase: session.PhaseStarting}
		if err := session.Write(runDir, sess); err != nil {
			return err
		}
		var (
			lista     atomic.Bool // arranque terminado: las señales van al bucle de sesión
			armado    atomic.Bool // bloqueo tomado: desde aquí hay cosas que desmontar
			abortando atomic.Bool // el manejador de señales está desmontando y va a salir
		)
		loopSig := make(chan os.Signal, 8)
		go func() {
			for sig := range sigc {
				if lista.Load() {
					select {
					case loopSig <- sig:
					default: // bucle ocupado y cola llena: la orden ya está encolada
					}
					continue
				}
				if sig == syscall.SIGUSR1 {
					fmt.Println("· todavía arrancando: se ignora la petición de snapshot")
					continue
				}
				if abortando.Swap(true) {
					continue // ya se está desmontando
				}
				fmt.Printf("\n· recibido %s durante el arranque: se cancela y se limpia (no hay cambios que guardar)...\n", sig)
				if armado.Load() {
					teardown(runDir, dock.Env(runDir))
					_ = lock.Release(cfg.VaultPath, host, os.Getpid())
				} else {
					session.Remove(runDir)
					_ = os.Remove(filepath.Join(runDir, "restic-pass"))
				}
				os.Exit(130)
			}
		}()
		// Si el arranque falla, no se deja nada a medias: ni daemon, ni
		// contenedores, ni secretos/volcados restaurados en la tmpfs.
		arrancado := false
		defer func() {
			if arrancado {
				return
			}
			if abortando.Load() {
				select {} // el manejador de señales termina la limpieza y sale
			}
			if armado.Load() {
				fmt.Fprintln(os.Stderr, "· el arranque ha fallado: se desmonta todo para no dejar datos en claro en RAM")
				teardown(runDir, dock.Env(runDir))
			} else {
				session.Remove(runDir)
				_ = os.Remove(filepath.Join(runDir, "restic-pass"))
			}
		}()
		// 2. puerto libre (tras limpiar restos)
		if ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.Port)); err != nil {
			return fmt.Errorf("puerto 127.0.0.1:%d ocupado: %w (cambia port en la config)", cfg.Port, err)
		} else {
			_ = ln.Close()
		}
		// 3. desbloquear el almacén ANTES de tocar nada: frase incorrecta → paro aquí
		se, err := mustStoreEnv(cfg, runDir)
		if err != nil {
			return err
		}
		fmt.Println("✔ almacén desbloqueado:", cfg.VaultPath)
		// 4. bloqueo entre equipos
		if err := lock.Acquire(cfg.VaultPath, host, os.Getpid(), boot); err != nil {
			return err
		}
		armado.Store(true)
		fmt.Println("✔ sin bloqueo de otro equipo · bloqueo tomado por", host)
		defer lock.Release(cfg.VaultPath, host, os.Getpid())
		// 5. snapshot a restaurar: PRIMERO recuperar los secretos del snapshot
		//    (antes de crear los contenedores: si no, neo4j crea auth.ini con el
		//    pass provisional y luego el healthcheck falla al cambiar el env)
		secFile := filepath.Join(runDir, "secrets.env")
		snapID := upSnapshot
		restored := ""
		restoreDir := filepath.Join(runDir, "restore")
		if snapID == "" {
			snapID, err = store.Latest(se)
			if err != nil {
				return err
			}
		}
		if snapID != "" {
			if err := os.RemoveAll(restoreDir); err != nil {
				return err
			}
			if err := store.Restore(se, snapID, restoreDir); err != nil {
				return err
			}
			secPath, err := snap.FindFile(restoreDir, "secrets.env")
			if err != nil {
				return fmt.Errorf("el snapshot %s no trae secrets.env: no es un snapshot de flowsint-desk o está incompleto; prueba otro con `up --snapshot ID` (ver `flowsint-desk snapshots`)", short(snapID))
			}
			b, err := os.ReadFile(secPath)
			if err != nil {
				return err
			}
			if err := os.WriteFile(secFile, b, 0o600); err != nil {
				return err
			}
			restored = short(snapID)
			fmt.Printf("✔ secretos recuperados del snapshot %s\n", restored)
		} else {
			if _, err := os.Stat(secFile); os.IsNotExist(err) {
				sec, err := secrets.Generate()
				if err != nil {
					return err
				}
				if err := secrets.WriteEnv(secFile, sec); err != nil {
					return err
				}
				fmt.Println("· no hay snapshots: secretos provisionales nuevos en tmpfs")
			}
		}
		// 6. compose + env-file en el run dir
		stackDir := filepath.Join(runDir, "stack")
		if err := os.WriteFile(filepath.Join(stackDir, "compose.yml"), assets.ComposeYML, 0o600); err != nil {
			return err
		}
		// socket de Docker que verán api/celery: el real solo en modo "directo"
		enrichersSock := run.Socket(runDir)
		if cfg.DockerSocket != sockproxy.ModeDirect {
			enrichersSock = filepath.Join(runDir, "enrichers.sock")
		}
		writeComposeEnv := func() error {
			sec, err := secrets.ReadEnv(secFile)
			if err != nil {
				return err
			}
			ce := map[string]string{"DESK_RUN": runDir, "DESK_PORT": fmt.Sprint(cfg.Port), "DESK_SOCK": enrichersSock, "FLOWSINT_VERSION": cfg.FlowsintVersion}
			for k, v := range sec {
				ce[k] = v
			}
			return secrets.WriteEnv(filepath.Join(runDir, "compose.env"), ce)
		}
		if err := writeComposeEnv(); err != nil {
			return err
		}
		// 7. daemon rootless + imágenes
		env := dock.Env(runDir)
		redactVals = redactValues(runDir) // secretos + frase, para sanear salidas
		logf := func(f string, a ...any) { fmt.Printf("· "+f+"\n", a...) }
		if err := dock.StartDaemon(runDir); err != nil {
			return err
		}
		if dock.IsPodman() {
			fmt.Println("✔ Podman rootless listo (almacén en tmpfs, sin daemon)")
		} else {
			fmt.Println("✔ Docker rootless vivo (data-root en tmpfs)")
		}
		switch cfg.DockerSocket {
		case sockproxy.ModeDirect:
			fmt.Println("⚠ docker_socket = \"directo\": Flowsint ve el socket de Docker sin filtrar (un fallo en Flowsint daría acceso a tus ficheros)")
		default:
			px, err := sockproxy.Start(enrichersSock, run.Socket(runDir), cfg.DockerSocket == sockproxy.ModeOff,
				func(l string) { fmt.Fprintln(os.Stderr, "⚠", l) })
			if err != nil {
				return fmt.Errorf("arrancando el proxy filtrado del socket de Docker: %w", err)
			}
			defer px.Close()
			if cfg.DockerSocket == sockproxy.ModeOff {
				fmt.Println("✔ acceso de Flowsint a Docker desactivado (los enrichers que lanzan contenedores no funcionarán)")
			} else {
				fmt.Println("✔ socket de Docker filtrado para los enrichers (sin montajes, sin privilegios)")
			}
		}
		for _, stale := range staleDirs {
			if err := dock.WipeSubdirs(env, stale, "dockerd-data", "dockerd-exec", "rk-state", "neo4j-import"); err != nil {
				fmt.Fprintln(os.Stderr, "aviso: no se pudo barrer el directorio viejo:", err)
			} else {
				_ = os.RemoveAll(stale)
			}
		}
		if len(staleDirs) > 0 {
			fmt.Println("✔ restos de sesiones anteriores eliminados")
		}
		cacheDir, err := dock.CacheDir()
		if err != nil {
			return err
		}
		images := dock.Images(cfg.FlowsintVersion)
		if err := dock.EnsureImages(env, cacheDir, images, logf); err != nil {
			return err
		}
		fmt.Println("✔ imágenes listas")
		digests := dock.ImageDigests(env, cacheDir, images)
		// ¿este equipo ejecuta las mismas imágenes que crearon el snapshot? Con
		// etiquetas móviles (latest) torre y portátil pueden divergir, y la API
		// migra el esquema al arrancar: una imagen más vieja restauraría un
		// volcado de un esquema más nuevo.
		if snapID != "" {
			if m, err := snap.LoadAndVerify(restoreDir); err == nil {
				if dif := dock.Drift(m.ImageDigests, digests); len(dif) > 0 {
					fmt.Fprintf(os.Stderr, "⚠ este equipo NO ejecuta las mismas imágenes que crearon el snapshot (%s, host %s):\n", restored, m.Host)
					for _, img := range dif {
						fmt.Fprintln(os.Stderr, "    ·", img)
					}
					fmt.Fprintln(os.Stderr, "  Para igualarlas: cierra con `flowsint-desk down` y ejecuta `flowsint-desk images sync` (ver `flowsint-desk images`).")
				}
			}
		}
		// 7b. limpiar restos del proyecto en este motor (fallos previos, kill -9):
		//     volúmenes viejos (p. ej. auth.ini de Neo4j con otra contraseña) rompen el arranque
		dock.StackReset(env, runDir)
		// 8. bases de datos
		fmt.Println("· levantando postgres, redis, neo4j...")
		if out, err := dock.StackUp(env, runDir, "postgres", "redis", "neo4j"); err != nil {
			dumpContainerLogs(env, runDir, []string{"flowsint-desk-postgres", "flowsint-desk-redis", "flowsint-desk-neo4j"}, 150)
			return fmt.Errorf("arranque de las bases de datos: %s\n%s", redactText(err.Error(), redactVals), redactText(out, redactVals))
		}
		fmt.Println("✔ postgres, redis y neo4j healthy")
		// 9. restaurar los datos del snapshot en las BD vacías y verificar manifest
		if snapID == "" {
			fmt.Println("· no hay snapshots todavía: BD nueva")
		} else {
			if err := snap.RestoreInto(env, runDir, restoreDir); err != nil {
				// NO arrancar la interfaz: al salir se desmonta todo (defer)
				return fmt.Errorf("%s (NO se arranca la interfaz; prueba un snapshot anterior con `up --snapshot ID`)", redactText(err.Error(), redactVals))
			}
			fmt.Printf("✔ restaurado snapshot %s (manifest verificado)\n", restored)
		}
		// 10. api, celery, app
		fmt.Println("· levantando api, celery y app...")
		if out, err := dock.StackUp(env, runDir, "api", "celery", "app"); err != nil {
			dumpContainerLogs(env, runDir, []string{"flowsint-desk-api", "flowsint-desk-celery", "flowsint-desk-app"}, 150)
			return fmt.Errorf("arranque de Flowsint: %s\n%s", redactText(err.Error(), redactVals), redactText(out, redactVals))
		}
		// 11. sesión
		sess.Phase = session.PhaseReady
		if err := session.Write(runDir, sess); err != nil {
			return err
		}
		// desde aquí las señales las atiende el bucle de sesión y un fallo ya
		// no desmonta nada por su cuenta
		arrancado = true
		lista.Store(true)
		fmt.Printf("✔ Flowsint listo en http://127.0.0.1:%d  (Ctrl+C para guardar y cerrar)\n", cfg.Port)

		// 11b. apagado del equipo: inhibidor "delay" de logind + aviso de que el
		//      apagado empieza, para lanzar el snapshot final con pCloud aún vivo.
		soltarApagado := func() {}
		if rel, err := watchShutdown(func() {
			fmt.Println("\n· el equipo se apaga: snapshot final ahora (logind espera)...")
			select {
			case loopSig <- syscall.SIGTERM:
			default:
			}
		}); err == nil {
			soltarApagado = rel
			if w := inhibitDelaySeconds(); w >= 0 && w < minShutdownWindow {
				fmt.Printf("✔ apagado vigilado, pero logind solo espera %d s: puede no dar tiempo al snapshot final (ver `flowsint-desk doctor`)\n", w)
			} else {
				fmt.Println("✔ apagado vigilado (el snapshot final se lanza al empezar a apagar)")
			}
		} else if rel, err2 := legacyInhibit(); err2 == nil {
			soltarApagado = rel
			fmt.Println("✔ systemd-inhibit activo (sin aviso previo de apagado: no hay D-Bus de sistema)")
		} else {
			fmt.Fprintln(os.Stderr, "aviso: no se pudo vigilar el apagado del equipo; al apagar valdrá el último snapshot automático:", err)
		}
		defer soltarApagado()
		// 12. bucle de sesión: snapshot automático cada N min, SIGUSR1 = snapshot
		//     inmediato, latido del bloqueo cada minuto, SIGINT/SIGTERM = snapshot
		//     final y cierre ordenado.
		ultimoBueno := time.Now() // el estado recién restaurado ES el último snapshot
		doSnapshot := func(kind string) error {
			if !lock.Mine(cfg.VaultPath, host, os.Getpid()) {
				return fmt.Errorf("el bloqueo del almacén ya no es nuestro (otro equipo lo cogió mientras pCloud sincronizaba, o pCloud no deja leerlo): DEJO DE GUARDAR para no pisar sus datos")
			}
			m, err := snap.Build(env, runDir, filepath.Join(runDir, "staging"), digests, kind)
			if err != nil {
				return fmt.Errorf("preparando el volcado: %s", redactText(err.Error(), redactVals))
			}
			id, err := store.Backup(se, filepath.Join(runDir, "staging"), map[string]string{
				"host": host, "boot": short(boot), "tipo": kind,
			})
			if err != nil {
				return fmt.Errorf("%s", redactText(err.Error(), redactVals))
			}
			ultimoBueno = time.Now()
			_ = os.Remove(finalFailedMarker(runDir))
			fmt.Printf("✔ snapshot %s guardado (%s, %d nodos, %d rels, %d tablas pg)\n",
				short(id), kind, m.Neo4jTotalN, m.Neo4jTotalR, len(m.PGRows))
			// retención: como mucho una vez al día
			if cfg.ForgetDue() {
				if out, err := store.Forget(se, cfg.KeepHourly, cfg.KeepDaily, cfg.KeepMonthly, true); err != nil {
					fmt.Fprintln(os.Stderr, "aviso: forget falló:", redactText(err.Error(), redactVals))
				} else {
					// releer antes de guardar: no pisar lo que el usuario haya
					// editado en config.toml con la sesión abierta
					cfg.MarkForgotten()
					if fresh, err := config.Load(); err == nil {
						fresh.LastForget = cfg.LastForget
						_ = fresh.Save()
					}
					fmt.Println("· retención aplicada (forget+prune):", strings.TrimSpace(out))
				}
			}
			return nil
		}
		avisar := func(kind string, err error) {
			fmt.Fprintf(os.Stderr, "⚠ el snapshot (%s) FALLÓ: %v\n  Lo trabajado desde el último snapshot bueno (hace %s) NO está guardado todavía.\n",
				kind, err, time.Since(ultimoBueno).Round(time.Second))
		}
		saveTick := time.NewTicker(time.Duration(cfg.SaveIntervalMin) * time.Minute)
		hbTick := time.NewTicker(1 * time.Minute)
		defer saveTick.Stop()
		defer hbTick.Stop()
		var cierreFallido time.Time // último intento de cierre cuyo snapshot final falló
		for {
			select {
			case sig := <-loopSig:
				if sig == syscall.SIGUSR1 {
					fmt.Println("· snapshot solicitado (SIGUSR1)...")
					if err := doSnapshot("manual"); err != nil {
						avisar("manual", err)
					}
					continue
				}
				fmt.Printf("\n· recibido %s: snapshot final y cierre...\n", sig)
				if err := doSnapshot("final"); err != nil {
					// Cerrar ahora borraría de la RAM lo único que queda del
					// trabajo no guardado. Solo se cierra sin guardar si el
					// usuario insiste dentro del plazo de gracia.
					if time.Since(cierreFallido) > closeGrace {
						cierreFallido = time.Now()
						_ = os.WriteFile(finalFailedMarker(runDir), nil, 0o600)
						// si esto era un apagado, no tiene sentido retenerlo más
						soltarApagado()
						avisar("final", err)
						fmt.Fprintf(os.Stderr, "✖ NO se cierra: la sesión SIGUE ABIERTA en http://127.0.0.1:%d para no perder ese trabajo.\n"+
							"  · Arregla la causa (¿pCloud montado y con sesión?, ¿espacio?, ¿bloqueo?) y repite Ctrl+C o `flowsint-desk down`.\n"+
							"  · Para cerrar SIN guardar, repite Ctrl+C o `flowsint-desk down` antes de %s.\n",
							cfg.Port, closeGrace)
						continue
					}
					fmt.Fprintf(os.Stderr, "⚠ el snapshot final ha vuelto a fallar (%v): se cierra SIN guardar, como has pedido. Vale el último snapshot bueno (hace %s).\n",
						err, time.Since(ultimoBueno).Round(time.Second))
				}
				teardown(runDir, env)
				return nil
			case <-saveTick.C:
				if err := doSnapshot("auto"); err != nil {
					avisar("auto", err)
				}
			case <-hbTick.C:
				if err := lock.Touch(cfg.VaultPath, host, os.Getpid()); err != nil {
					fmt.Println("⚠ latido del bloqueo falló:", err)
				}
			}
		}
	},
}

// closeGrace es el plazo, tras un snapshot final fallido, en el que una
// segunda orden de cierre se obedece aunque no se pueda guardar.
const closeGrace = 2 * time.Minute

// finalFailedMarker es el fichero (tmpfs) con el que `up` le dice a `down`
// que el snapshot final falló y la sesión sigue abierta.
func finalFailedMarker(runDir string) string { return filepath.Join(runDir, "final-failed") }

// staleName devuelve un nombre único para apartar un directorio de ejecución
// muerto (casa con el glob /tmp/flowsint-desk-*.stale que barre `up`).
func staleName(runDir string) string {
	return fmt.Sprintf("%s.%d.stale", runDir, time.Now().UnixNano())
}

// teardown desmonta una sesión, esté como esté: tira el compose, barre el
// data-root desde dentro del user-ns, para el daemon rootless, mata lo que
// quede colgando de ese directorio y lo borra (tmpfs). Vale para el cierre
// normal, para un arranque fallido o cancelado y para los restos de un kill -9.
func teardown(runDir string, env []string) {
	stack := filepath.Join(runDir, "stack", "compose.yml")
	if dock.Alive(env) {
		if fileExists(stack) && fileExists(filepath.Join(runDir, "compose.env")) {
			if out, err := dock.StackDown(env, runDir); err != nil {
				fmt.Fprintln(os.Stderr, "aviso: parando los contenedores:", err, strings.TrimSpace(redactText(out, redactValues(runDir))))
			}
		}
		if err := dock.WipeSubdirs(env, runDir, "dockerd-data", "dockerd-exec", "rk-state", "neo4j-import"); err != nil {
			fmt.Fprintln(os.Stderr, "aviso: wipe del data-root:", err)
		}
	}
	dock.StopDaemon(runDir)
	// por si el daemon no llegó a escribir su pidfile o dejó hijos vivos
	dock.KillOrphanDaemons(runDir + "/")
	session.Remove(runDir)
	if err := run.Cleanup(runDir); err != nil {
		// típico: ficheros de uids mapeados que solo se borran desde dentro
		// de un daemon rootless. Se apartan para que el siguiente `up` los barra.
		stale := staleName(runDir)
		if os.Rename(runDir, stale) == nil {
			fmt.Fprintf(os.Stderr, "aviso: no se pudo borrar todo el directorio de ejecución (%v).\n  Quedan restos en %s (tmpfs: RAM, no disco); el siguiente `flowsint-desk up` los elimina, y un reinicio también.\n", err, stale)
		} else {
			fmt.Fprintln(os.Stderr, "aviso: no se pudo borrar el directorio de ejecución:", err)
		}
		return
	}
	fmt.Println("✔ contenedores abajo, motor parado, directorio de ejecución borrado (nada en claro en disco)")
}

func init() {
	upCmd.Flags().StringVar(&upSnapshot, "snapshot", "", "arrancar desde un snapshot antiguo (ID)")
}
