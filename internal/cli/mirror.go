package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"flowsint-desk/internal/config"
	"flowsint-desk/internal/run"
	"flowsint-desk/internal/store"

	"github.com/spf13/cobra"
)

var mirrorTo string

var mirrorCmd = &cobra.Command{
	Use:   "mirror [--to RUTA]",
	Short: "Segunda copia del almacén en otro sitio (p. ej. un disco externo)",
	Long: `Copia todos los snapshots a un segundo repositorio restic, cifrado con la
misma frase de paso. Hoy todo depende de una única carpeta en pCloud: si esa
cuenta se pierde, se corrompe o se borra por error, no hay más. Con una segunda
copia en un disco que controlas tienes dos sitios y dos soportes.

La primera vez indica dónde:   flowsint-desk mirror --to /run/media/tu/DISCO/flowsint-mirror
Después basta:                 flowsint-desk mirror

Es incremental (solo copia lo nuevo). Si la ruta está configurada y accesible,
"up" actualiza la segunda copia sola al cerrar la sesión. Para restaurar desde
ella: "flowsint-desk init --vault <ruta de la copia>" en cualquier equipo.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		dest := cfg.MirrorPath
		if mirrorTo != "" {
			if dest, err = expandPath(mirrorTo); err != nil {
				return err
			}
		}
		if dest == "" {
			return fmt.Errorf("no hay segunda copia configurada: indica dónde con `flowsint-desk mirror --to RUTA`")
		}
		if err := mirrorDestOK(cfg.VaultPath, dest); err != nil {
			return err
		}
		runDir, err := run.Ensure()
		if err != nil {
			return err
		}
		se, limpiar, err := openStore(cfg, runDir)
		if err != nil {
			return err
		}
		defer limpiar()
		if err := doMirror(se, dest); err != nil {
			return err
		}
		if mirrorTo != "" && dest != cfg.MirrorPath {
			if err := config.Update(func(c *config.Config) { c.MirrorPath = dest }); err != nil {
				return err
			}
			fmt.Println("✔ ruta guardada: a partir de ahora `up` actualizará esta copia al cerrar si el disco está conectado")
		}
		return nil
	},
}

// mirrorDestOK rechaza destinos que no son una segunda copia de verdad.
func mirrorDestOK(vault, dest string) error {
	v, d := filepath.Clean(vault), filepath.Clean(dest)
	if d == v || strings.HasPrefix(d+"/", v+"/") || strings.HasPrefix(v+"/", d+"/") {
		return fmt.Errorf("la segunda copia no puede estar dentro del almacén ni contenerlo (%s)", dest)
	}
	if _, err := os.Stat(filepath.Dir(d)); err != nil {
		return fmt.Errorf("no existe %s: ¿está conectado y montado el disco de la segunda copia?", filepath.Dir(d))
	}
	return nil
}

// doMirror actualiza la segunda copia e informa.
func doMirror(se store.Env, dest string) error {
	fmt.Println("· actualizando la segunda copia en", dest, "...")
	creado, err := store.Mirror(se, dest)
	if err != nil {
		return err
	}
	n, err := store.CountSnapshots(se, dest)
	if err != nil {
		return fmt.Errorf("la copia terminó pero no se puede leer: %w", err)
	}
	_ = config.MarkMirrored()
	if creado {
		fmt.Printf("✔ segunda copia creada: %d snapshots en %s\n", n, dest)
	} else {
		fmt.Printf("✔ segunda copia al día: %d snapshots en %s\n", n, dest)
	}
	return nil
}

func init() {
	mirrorCmd.Flags().StringVar(&mirrorTo, "to", "", "dónde guardar la segunda copia (se recuerda para las siguientes veces)")
}
