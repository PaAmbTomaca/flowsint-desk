package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"flowsint-desk/internal/config"
	"flowsint-desk/internal/dock"
	"flowsint-desk/internal/run"
	"flowsint-desk/internal/session"
	"flowsint-desk/internal/store"

	"github.com/spf13/cobra"
)

var imagesCmd = &cobra.Command{
	Use:   "images [pull|update|sync]",
	Short: "Qué imágenes exactas usa este equipo, y cómo descargarlas, actualizarlas o igualarlas a las del almacén",
	Long: `Sin argumento: muestra, para cada imagen, el digest que tiene este equipo en
su caché y el digest con el que se creó el último snapshot del almacén.

  pull     descarga a la caché las que falten (para que el primer "up" no espere)
  update   vuelve a descargar todas las etiquetas (actualización deliberada);
           después de guardar un snapshot, el otro equipo debe hacer "sync"
  sync     descarga, por digest, exactamente las imágenes del último snapshot:
           deja este equipo igual que el que guardó por última vez

Las imágenes van por etiqueta (flowsint_version en config.toml; "latest" por
defecto): sin fijarlas, cada equipo puede acabar con una versión distinta de
Flowsint. pull/update/sync necesitan que no haya ninguna sesión abierta.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		cacheDir, err := dock.CacheDir()
		if err != nil {
			return err
		}
		images := dock.Images(cfg.FlowsintVersion)
		op := ""
		if len(args) == 1 {
			op = args[0]
		}
		switch op {
		case "":
			return imagesStatus(cfg, cacheDir, images)
		case "pull":
			return imagesPull(cacheDir, images)
		case "update":
			for _, img := range images {
				dock.DropCache(cacheDir, img)
			}
			if err := imagesPull(cacheDir, images); err != nil {
				return err
			}
			fmt.Println("✔ imágenes actualizadas. Tras tu próximo snapshot, ejecuta `flowsint-desk images sync` en el otro equipo.")
			return nil
		case "sync":
			return imagesSync(cfg, cacheDir, images)
		}
		return fmt.Errorf("operación desconocida %q: usa pull, update o sync", op)
	},
}

func logDot(f string, a ...any) { fmt.Printf("· "+f+"\n", a...) }

// imagesPull descarga a la caché las imágenes que falten.
func imagesPull(cacheDir string, images []string) error {
	return withDaemon(func(env []string) error {
		if err := dock.EnsureImages(env, cacheDir, images, logDot); err != nil {
			return err
		}
		fmt.Println("✔ imágenes en caché:", cacheDir)
		return nil
	})
}

// imagesSync deja en la caché, por digest, las imágenes del último snapshot.
func imagesSync(cfg *config.Config, cacheDir string, images []string) error {
	want, quien, err := snapshotDigests(cfg)
	if err != nil {
		return err
	}
	have := dock.CachedDigests(cacheDir, images)
	var pendientes []string
	for _, img := range images {
		w := want[img]
		if w == "" {
			fmt.Printf("· %s: el snapshot no anota su digest (creado con una versión anterior o con otra etiqueta): no se toca\n", img)
			continue
		}
		if have[img] != "" && len(dock.Drift(map[string]string{img: w}, have)) == 0 {
			continue
		}
		pendientes = append(pendientes, img)
	}
	if len(pendientes) == 0 {
		fmt.Printf("✔ nada que igualar con el último snapshot (%s)\n", quien)
		return nil
	}
	return withDaemon(func(env []string) error {
		for _, img := range pendientes {
			logDot("descargando %s por digest", img)
			if err := dock.PullExact(env, cacheDir, img, want[img]); err != nil {
				return err
			}
		}
		fmt.Printf("✔ %d imágenes igualadas a las del último snapshot (%s)\n", len(pendientes), quien)
		return nil
	})
}

// snapshotDigests devuelve los digests de imagen anotados en el manifest del
// último snapshot, y una descripción de quién lo creó.
func snapshotDigests(cfg *config.Config) (map[string]string, string, error) {
	runDir, err := run.Ensure()
	if err != nil {
		return nil, "", err
	}
	se, limpiar, err := openStore(cfg, runDir)
	if err != nil {
		return nil, "", err
	}
	defer limpiar()
	snaps, err := store.Snapshots(se)
	if err != nil {
		return nil, "", err
	}
	if len(snaps) == 0 {
		return nil, "", fmt.Errorf("el almacén no tiene snapshots todavía")
	}
	last := snaps[len(snaps)-1]
	m, ok := manifestOf(se, last)
	if !ok {
		return nil, "", fmt.Errorf("no se pudo leer el manifest del snapshot %s", last.ShortID)
	}
	return m.ImageDigests, fmt.Sprintf("%s, host %s, %s", last.ShortID, m.Host, last.Time.Local().Format("2006-01-02 15:04")), nil
}

func imagesStatus(cfg *config.Config, cacheDir string, images []string) error {
	have := dock.CachedDigests(cacheDir, images)
	want, quien, err := snapshotDigests(cfg)
	if err != nil {
		fmt.Println("(no se pudo leer el último snapshot:", err.Error()+")")
		want = map[string]string{}
	} else {
		fmt.Println("último snapshot:", quien)
	}
	corto := func(ref string) string {
		if ref == "" {
			return "(desconocido)"
		}
		d := ref
		if i := len(ref) - 64; i > 0 {
			d = "sha256:" + ref[i:i+12] + "…"
		}
		return d
	}
	distintas := 0
	for _, img := range images {
		estado := "="
		switch {
		case have[img] == "" || want[img] == "":
			estado = "?"
		case len(dock.Drift(map[string]string{img: want[img]}, have)) > 0:
			estado = "≠"
			distintas++
		}
		fmt.Printf("  %s %-46s este equipo %-22s snapshot %s\n", estado, img, corto(have[img]), corto(want[img]))
	}
	switch {
	case distintas > 0:
		fmt.Printf("⚠ %d imágenes distintas de las del último snapshot: `flowsint-desk images sync` las iguala\n", distintas)
	default:
		fmt.Println("(= igual · ≠ distinta · ? digest no anotado: se anota al descargar con pull/update/sync)")
	}
	return nil
}

// withDaemon arranca un daemon rootless temporal en el directorio de
// ejecución (tmpfs), ejecuta fn y lo para dejando el directorio como estaba.
// Solo sin sesión: el daemon de una sesión viva no se toca.
func withDaemon(fn func(env []string) error) error {
	runDir, err := run.Ensure()
	if err != nil {
		return err
	}
	if s, err := session.Read(runDir); err == nil && s.Alive() {
		return fmt.Errorf("hay una sesión abierta (pid %d): ciérrala con `flowsint-desk down` antes de tocar las imágenes", s.PID)
	}
	if fileExists(run.Socket(runDir)) || fileExists(filepath.Join(runDir, "dockerd.pid")) {
		return fmt.Errorf("quedan restos de una sesión anterior: ejecuta `flowsint-desk down` y repite")
	}
	env := dock.Env(runDir)
	fmt.Println("· arrancando un Docker rootless temporal (tmpfs)...")
	if err := dock.StartDaemon(runDir); err != nil {
		stopTempDaemon(runDir, env)
		return err
	}
	defer stopTempDaemon(runDir, env)
	return fn(env)
}

// stopTempDaemon para el daemon temporal y borra SOLO lo suyo (no la frase ni
// los secretos que `init` pueda haber dejado para el primer `up`).
func stopTempDaemon(runDir string, env []string) {
	if dock.Alive(env) {
		if err := dock.WipeSubdirs(env, runDir, "dockerd-data", "dockerd-exec", "rk-state"); err != nil {
			fmt.Fprintln(os.Stderr, "aviso:", err)
		}
	}
	dock.StopDaemon(runDir)
	dock.KillOrphanDaemons(runDir + "/")
	for _, f := range []string{"docker.sock", "dockerd.pid", "dockerd-rootless.log", "dockerd-data", "dockerd-exec", "rk-state"} {
		_ = os.RemoveAll(filepath.Join(runDir, f))
	}
}
