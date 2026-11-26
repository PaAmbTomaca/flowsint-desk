package cli

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
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
			return fmt.Errorf("ya hay una sesión viva (pid %d, puerto %d); usa `flowsint-desk down`", old.PID, old.Port)
		}
		// 1b. limpieza de restos de un kill -9 / corte (ANTES de comprobar el puerto,
		//     porque un daemon huérfano puede tener el puerto ocupado):
		//     - todos los /tmp/flowsint-desk-*.stale: matar su daemon y sus contenedores
		//     - el run dir actual si tiene session.json muerta o socket vivo
		var staleDirs []string
		var redactVals []string // valores a enmascarar (***) en avisos y logs
		// daemons huérfanos cuyo socket ya no es alcanzable (dir renombrado):
		if k := dock.KillOrphanDaemons(); len(k) > 0 {
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
			fmt.Println("· detectados restos (.stale) de sesiones anteriores: se limpian")
			for _, s := range g {
				limpiarDir(s)
				staleDirs = append(staleDirs, s)
			}
		}
		_, statErr := os.Stat(run.Socket(runDir))
		_, sessErr := session.Read(runDir)
		if statErr == nil || sessErr == nil {
			fmt.Println("· detectados restos de una sesión anterior (kill -9 / corte): se limpian")
			limpiarDir(runDir)
			stale := runDir + ".stale"
			_ = os.Rename(runDir, stale)
			staleDirs = append(staleDirs, stale)
			runDir, err = run.Ensure()
			if err != nil {
				return err
			}
		}
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
		host, _ := os.Hostname()
		boot, _ := run.BootID()
		if err := lock.Acquire(cfg.VaultPath, host, os.Getpid(), boot); err != nil {
			return err
		}
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
			if secPath, err := snap.FindFile(restoreDir, "secrets.env"); err == nil {
				b, err := os.ReadFile(secPath)
				if err != nil {
					return err
				}
				if err := os.WriteFile(secFile, b, 0o600); err != nil {
					return err
				}
				restored = short(snapID)
				fmt.Printf("✔ secretos recuperados del snapshot %s\n", restored)
			}
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
		writeComposeEnv := func() error {
			sec, err := secrets.ReadEnv(secFile)
			if err != nil {
				return err
			}
			ce := map[string]string{"DESK_RUN": runDir, "DESK_PORT": fmt.Sprint(cfg.Port)}
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
		fmt.Println("✔ Docker rootless vivo (data-root en tmpfs)")
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
		if err := dock.EnsureImages(env, cacheDir, logf); err != nil {
			return err
		}
		fmt.Println("✔ imágenes listas")
		// 7b. limpiar restos del proyecto en este daemon (fallos previos, kill -9):
		//     volúmenes viejos (p. ej. auth.ini de Neo4j con otra contraseña) rompen el arranque
		_, _ = dock.Compose(env, runDir, "down", "-v", "--remove-orphans")
		for _, v := range []string{"pg_data", "neo4j_data", "neo4j_logs", "neo4j_plugins"} {
			_, _ = dock.Run(env, "docker", "volume", "rm", "flowsint-desk_"+v)
		}
		_, _ = dock.Run(env, "docker", "network", "rm", "flowsint-desk-net")
		// 8. bases de datos
		fmt.Println("· levantando postgres, redis, neo4j...")
		if out, err := dock.Compose(env, runDir, "up", "-d", "--wait", "postgres", "redis", "neo4j"); err != nil {
			dumpContainerLogs(env, runDir, []string{"flowsint-desk-postgres", "flowsint-desk-redis", "flowsint-desk-neo4j"}, 150)
			return fmt.Errorf("compose up (db): %v\n%s", err, redactText(out, redactVals))
		}
		fmt.Println("✔ postgres, redis y neo4j healthy")
		// 9. restaurar los datos del snapshot en las BD vacías y verificar manifest
		if snapID == "" {
			fmt.Println("· no hay snapshots todavía: BD nueva")
		} else {
			if err := snap.RestoreInto(env, runDir, restoreDir); err != nil {
				// NO arrancar la interfaz: bajar lo levantado y salir
				_, _ = dock.Compose(env, runDir, "down", "--remove-orphans")
				return fmt.Errorf("%w (he parado los servicios y NO he arrancado la interfaz)", err)
			}
			fmt.Printf("✔ restaurado snapshot %s (manifest verificado)\n", restored)
		}
		// 10. api, celery, app
		fmt.Println("· levantando api, celery y app...")
		if out, err := dock.Compose(env, runDir, "up", "-d", "--wait", "api", "celery", "app"); err != nil {
			dumpContainerLogs(env, runDir, []string{"flowsint-desk-api", "flowsint-desk-celery", "flowsint-desk-app"}, 150)
			return fmt.Errorf("compose up (app): %v\n%s", err, redactText(out, redactVals))
		}
		// 11. sesión
		sess := &session.Session{PID: os.Getpid(), Port: cfg.Port, Started: time.Now().UTC().Format(time.RFC3339), Host: host, BootID: boot}
		if err := session.Write(runDir, sess); err != nil {
			return err
		}
		fmt.Printf("✔ Flowsint listo en http://127.0.0.1:%d  (Ctrl+C para guardar y cerrar)\n", cfg.Port)

		// 11b. systemd-inhibit: retrasar el apagado para poder hacer el snapshot
		//      final si no da tiempo; si no, vale el último snapshot automático.
		var inhibit *exec.Cmd
		if _, err := exec.LookPath("systemd-inhibit"); err == nil {
			inhibit = exec.Command("systemd-inhibit",
				"--what=shutdown", "--mode=delay",
				"--who=flowsint-desk", "--why=sesión flowsint-desk activa (snapshot final al apagar)",
				"sleep", "infinity")
			if err := inhibit.Start(); err == nil {
				fmt.Println("✔ systemd-inhibit activo (el apagado esperará al snapshot final)")
				defer func() { _ = inhibit.Process.Kill() }()
			} else {
				fmt.Fprintln(os.Stderr, "aviso: no se pudo activar systemd-inhibit:", err)
			}
		}
		// 12. bucle de sesión: snapshot automático cada N min, SIGUSR1 = snapshot
		//     inmediato, latido del bloqueo cada minuto, SIGINT/SIGTERM = snapshot
		//     final y cierre ordenado.
		digests, _ := dock.ImageDigests(env)
		doSnapshot := func(kind string) {
			if !lock.Mine(cfg.VaultPath, host, os.Getpid()) {
				fmt.Println("⚠ ¡ATENCIÓN! el bloqueo ya no es nuestro (otro equipo lo cogió mientras pCloud sincronizaba): DEJO DE GUARDAR")
				return
			}
			m, err := snap.Build(env, runDir, filepath.Join(runDir, "staging"), digests, kind)
			if err != nil {
				fmt.Fprintln(os.Stderr, "aviso: snapshot falló:", redactText(err.Error(), redactVals))
				return
			}
			id, err := store.Backup(se, filepath.Join(runDir, "staging"), map[string]string{
				"host": host, "boot": short(boot), "tipo": kind,
			})
			if err != nil {
				fmt.Fprintln(os.Stderr, "aviso: restic backup falló:", redactText(err.Error(), redactVals))
				return
			}
			fmt.Printf("✔ snapshot %s guardado (%s, %d nodos, %d rels, %d tablas pg)\n",
				short(id), kind, m.Neo4jTotalN, m.Neo4jTotalR, len(m.PGRows))
			// retención: como mucho una vez al día
			if cfg.ForgetDue() {
				if out, err := store.Forget(se, cfg.KeepHourly, cfg.KeepDaily, cfg.KeepMonthly, true); err != nil {
					fmt.Fprintln(os.Stderr, "aviso: forget falló:", redactText(err.Error(), redactVals))
				} else {
					cfg.MarkForgotten()
					_ = cfg.Save()
					fmt.Println("· retención aplicada (forget+prune):", strings.TrimSpace(out))
				}
			}
		}
		sigc := make(chan os.Signal, 4)
		signal.Notify(sigc, syscall.SIGINT, syscall.SIGTERM, syscall.SIGUSR1)
		saveTick := time.NewTicker(time.Duration(cfg.SaveIntervalMin) * time.Minute)
		hbTick := time.NewTicker(1 * time.Minute)
		defer saveTick.Stop()
		defer hbTick.Stop()
		for {
			select {
			case sig := <-sigc:
				if sig == syscall.SIGUSR1 {
					fmt.Println("· snapshot solicitado (SIGUSR1)...")
					doSnapshot("manual")
					continue
				}
				fmt.Printf("\n· recibido %s: snapshot final y cierre...\n", sig)
				doSnapshot("final")
				shutdown(runDir, env)
				return nil
			case <-saveTick.C:
				doSnapshot("auto")
			case <-hbTick.C:
				if err := lock.Touch(cfg.VaultPath, host, os.Getpid()); err != nil {
					fmt.Println("⚠ latido del bloqueo falló:", err)
				}
			}
		}
	},
}

// shutdown tira el compose, barre el data-root desde dentro del user-ns,
// para el daemon rootless y borra el directorio de ejecución (tmpfs).
func shutdown(runDir string, env []string) {
	if out, err := dock.Compose(env, runDir, "down", "--remove-orphans"); err != nil {
		fmt.Fprintln(os.Stderr, "aviso: compose down:", err, strings.TrimSpace(out))
	}
	if err := dock.WipeSubdirs(env, runDir, "dockerd-data", "dockerd-exec", "rk-state", "neo4j-import"); err != nil {
		fmt.Fprintln(os.Stderr, "aviso: wipe del data-root:", err)
	}
	dock.StopDaemon(runDir)
	session.Remove(runDir)
	if err := run.Cleanup(runDir); err != nil {
		fmt.Fprintln(os.Stderr, "aviso: no se pudo borrar el directorio de ejecución:", err)
		return
	}
	fmt.Println("✔ compose abajo, Docker rootless parado, directorio de ejecución borrado (nada en claro en disco)")
}

func init() {
	upCmd.Flags().StringVar(&upSnapshot, "snapshot", "", "arrancar desde un snapshot antiguo (ID)")
}
