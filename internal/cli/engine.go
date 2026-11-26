package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"flowsint-desk/internal/config"
	"flowsint-desk/internal/dock"
	"flowsint-desk/internal/passphrase"
	"flowsint-desk/internal/run"
	"flowsint-desk/internal/session"

	"github.com/spf13/cobra"
)

// engineWarning es el aviso que se muestra al elegir motor y al cambiarlo.
const engineWarning = `  AVISO — el motor se elige una vez por equipo. Cambiarlo después NO es un
  ajuste en caliente: hay que cerrar la sesión y ejecutar
      flowsint-desk engine <docker|podman>
  que borra la caché de imágenes del motor anterior y te manda repetir
  "flowsint-desk setup" (requisitos del motor nuevo, imágenes y prueba de humo).
  Tus datos NO se tocan ni se pierden: viven cifrados en el almacén y no
  dependen del motor. Los paquetes del motor anterior puedes desinstalarlos tú
  si ya no los usas para nada más (la CLI nunca desinstala nada).`

func engineInstalled(e string) bool {
	_, err := exec.LookPath(e)
	return err == nil
}

func engineBlurb(e string) string {
	if e == dock.EnginePodman {
		return "Podman rootless — sin daemon; un solo paquete; la CLI lanza los contenedores ella misma y la limpieza no depende de ningún servicio. Motor NUEVO en flowsint-desk."
	}
	return "Docker rootless — dockerd por sesión + docker compose; más piezas que instalar (rootlesskit, slirp4netns, fuse-overlayfs...). Es el motor con el que se construyó y validó el proyecto."
}

// chooseEngine decide el motor en `setup`: el de --engine, el ya configurado,
// o (primera vez, con terminal) lo que elija el usuario.
func chooseEngine(cfg *config.Config, flag string, cfgExisted bool) (string, error) {
	if flag != "" {
		if !dock.ValidEngine(flag) {
			return "", fmt.Errorf("motor desconocido %q: usa docker o podman", flag)
		}
		if cfgExisted && flag != cfg.Engine {
			return "", fmt.Errorf("este equipo ya está configurado con %s. Para cambiar de motor: `flowsint-desk engine %s`", cfg.Engine, flag)
		}
		return flag, nil
	}
	if cfgExisted {
		return cfg.Engine, nil
	}
	// primera vez: proponer según lo instalado
	def := cfg.Engine
	if !engineInstalled(dock.EngineDocker) && engineInstalled(dock.EnginePodman) {
		def = dock.EnginePodman
	}
	if setupYes || !interactive() {
		return def, nil
	}
	fmt.Println("Elige el motor de contenedores para este equipo:")
	for i, e := range []string{dock.EngineDocker, dock.EnginePodman} {
		inst := "no instalado"
		if engineInstalled(e) {
			inst = "instalado"
		}
		fmt.Printf("  %d) %s [%s]\n     %s\n", i+1, e, inst, engineBlurb(e))
	}
	fmt.Println(engineWarning)
	n := "1"
	if def == dock.EnginePodman {
		n = "2"
	}
	for {
		fmt.Fprintf(os.Stderr, "Motor [%s]: ", n)
		switch ans := passphrase.ReadLine(); ans {
		case "":
			return def, nil
		case "1", dock.EngineDocker:
			return dock.EngineDocker, nil
		case "2", dock.EnginePodman:
			return dock.EnginePodman, nil
		}
	}
}

var engineCmd = &cobra.Command{
	Use:   "engine [docker|podman]",
	Short: "Muestra el motor de contenedores de este equipo, o lo cambia",
	Long: `Sin argumento muestra el motor configurado (docker o podman).

Con argumento cambia de motor. No es un ajuste en caliente: exige que no haya
sesión abierta, borra la caché de imágenes (cada motor guarda las suyas) y deja
el equipo pendiente de repetir "flowsint-desk setup" para comprobar los
requisitos del motor nuevo, descargar imágenes y pasar la prueba de humo.
Los datos no se tocan: están cifrados en el almacén y no dependen del motor.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		if len(args) == 0 {
			fmt.Printf("motor de contenedores: %s\n  %s\n", cfg.Engine, engineBlurb(cfg.Engine))
			for _, e := range []string{dock.EngineDocker, dock.EnginePodman} {
				if e != cfg.Engine {
					fmt.Printf("para cambiar: flowsint-desk engine %s\n", e)
				}
			}
			return nil
		}
		nuevo := args[0]
		if !dock.ValidEngine(nuevo) {
			return fmt.Errorf("motor desconocido %q: usa docker o podman", nuevo)
		}
		if nuevo == cfg.Engine && os.Getenv(config.EnvEngine) == "" {
			fmt.Println("este equipo ya usa", nuevo)
			return nil
		}
		runDir, err := run.Dir()
		if err != nil {
			return err
		}
		if s, err := session.Read(runDir); err == nil && s.Alive() {
			return fmt.Errorf("hay una sesión abierta (pid %d): ciérrala con `flowsint-desk down` antes de cambiar de motor", s.PID)
		}
		if fileExists(run.Socket(runDir)) || fileExists(filepath.Join(runDir, "dockerd.pid")) {
			return fmt.Errorf("quedan restos de una sesión del motor actual: ejecuta `flowsint-desk down` y repite")
		}
		fmt.Printf("Vas a cambiar el motor de este equipo: %s → %s\n%s\n", cfg.Engine, nuevo, engineWarning)
		if !askYN("¿Cambiar de motor?", false) {
			return fmt.Errorf("cancelado: el motor sigue siendo %s", cfg.Engine)
		}
		cfg.Engine = nuevo
		if err := cfg.Save(); err != nil {
			return err
		}
		if cacheDir, err := dock.CacheDir(); err == nil {
			if err := os.RemoveAll(cacheDir); err != nil {
				fmt.Fprintln(os.Stderr, "aviso: no se pudo borrar la caché de imágenes:", err)
			} else {
				fmt.Println("✔ caché de imágenes del motor anterior borrada")
			}
		}
		fmt.Printf("✔ motor cambiado a %s. Ahora ejecuta:  flowsint-desk setup\n", nuevo)
		return nil
	},
}
