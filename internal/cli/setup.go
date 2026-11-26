package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"flowsint-desk/internal/config"
	"flowsint-desk/internal/dock"
	"flowsint-desk/internal/passphrase"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	setupYes    bool
	setupVault  string
	setupEngine string
)

// interactive: hay alguien delante para contestar preguntas.
func interactive() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// askYN pregunta sí/no. Sin terminal (o con --yes) devuelve el valor por defecto.
func askYN(q string, def bool) bool {
	if setupYes || !interactive() {
		return def
	}
	op := "[S/n]"
	if !def {
		op = "[s/N]"
	}
	fmt.Fprintf(os.Stderr, "%s %s: ", q, op)
	switch strings.ToLower(passphrase.ReadLine()) {
	case "":
		return def
	case "s", "si", "sí", "y", "yes":
		return true
	}
	return false
}

func paso(n int, titulo string) { fmt.Printf("\n── Paso %d · %s ──\n", n, titulo) }

// setupCmd es el asistente: deja el equipo listo de principio a fin.
var setupCmd = &cobra.Command{
	Use:   "setup [--yes] [--engine docker|podman] [--vault RUTA]",
	Short: "Asistente: prepara este equipo de principio a fin (requisitos, almacén, imágenes y prueba)",
	Long: `Guía paso a paso hasta dejar flowsint-desk funcionando en este equipo:

  0. Motor: la primera vez eliges Docker rootless o Podman rootless.
  1. Requisitos: comprueba todo, instala lo que no necesita sudo (restic con su
     sha256, los binarios rootless con su firma) y, para lo que sí lo necesita,
     te da el comando exacto de TU distribución y espera a que lo ejecutes.
  2. Almacén: localiza pCloud Drive, propone la ruta y detecta si este es el
     primer equipo (crea el almacén) o el segundo (se conecta al que ya hay,
     sin crear nada).
  3. Imágenes: las descarga a la caché; en un segundo equipo, las MISMAS (por
     digest) que usó el equipo que guardó por última vez.
  4. Prueba: opcionalmente, un ciclo completo con un almacén desechable.
  5. Arranque.

Se puede repetir las veces que haga falta: lo que ya está bien, se salta.
La CLI nunca ejecuta sudo. Con --yes (o sin terminal) no pregunta: acepta los
valores por defecto y se detiene en el primer paso que requiera tu intervención.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// ── 0. motor de contenedores (una vez por equipo) ──
		cfg0, err := config.Load()
		if err != nil {
			return err
		}
		cfgExisted := fileExists(cfg0.Path)
		motor, err := chooseEngine(cfg0, setupEngine, cfgExisted)
		if err != nil {
			return err
		}
		if !cfgExisted || cfg0.Engine != motor {
			cfg0.Engine = motor
			if err := cfg0.Save(); err != nil {
				return err
			}
		}
		dock.SetEngine(motor)
		fmt.Printf("· motor de contenedores: %s (para cambiarlo más adelante: `flowsint-desk engine`)\n", motor)

		// ── 1. requisitos ──
		paso(1, "Requisitos del sistema")
		pendientes, _ := runDoctor(true)
		for {
			var bloquean []*doctorCheck
			for _, c := range pendientes {
				if !c.afterInit {
					bloquean = append(bloquean, c)
				}
			}
			if len(bloquean) == 0 {
				break
			}
			fmt.Println("\n→ falta lo siguiente, que la CLI no puede hacer por ti (nunca usa sudo). Cópialo en otra terminal:")
			for _, c := range bloquean {
				fmt.Printf("  ✖ %s\n     %s\n", c.name, c.fix)
			}
			if setupYes || !interactive() {
				return fmt.Errorf("quedan %d requisitos por resolver a mano; cuando estén, repite `flowsint-desk setup`", len(bloquean))
			}
			fmt.Fprint(os.Stderr, "\nCuando lo hayas hecho pulsa Intro para volver a comprobar (o escribe «salir»): ")
			if strings.EqualFold(passphrase.ReadLine(), "salir") {
				return fmt.Errorf("asistente interrumpido; repite `flowsint-desk setup` cuando quieras")
			}
			var siguen []*doctorCheck
			for _, c := range bloquean {
				if ok, det := c.run(); ok {
					printCheck("✔", c.name, det)
				} else {
					siguen = append(siguen, c)
				}
			}
			pendientes = siguen
		}
		fmt.Println("✔ requisitos del sistema listos")

		// ── 2. almacén ──
		paso(2, "Almacén cifrado en pCloud")
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		nuevoEquipo := true
		if cfgExisted && setupVault == "" && repoIn(cfg.VaultPath) {
			fmt.Println("✔ este equipo ya está conectado al almacén:", cfg.VaultPath)
			nuevoEquipo = false
		} else {
			for setupVault == "" && detectPCloud() == "" {
				fmt.Println("⚠ no veo pCloud Drive montado. Abre el cliente de pCloud e inicia sesión.")
				if setupYes || !interactive() {
					return fmt.Errorf("pCloud no está montado; ábrelo y repite `flowsint-desk setup`")
				}
				fmt.Fprint(os.Stderr, "Pulsa Intro para volver a mirar (o escribe «seguir» para indicar la ruta a mano): ")
				if strings.EqualFold(passphrase.ReadLine(), "seguir") {
					break
				}
			}
			if m := detectPCloud(); m != "" {
				fmt.Println("✔ pCloud Drive montado en", m)
			}
			vault := suggestedVault(cfg.VaultPath)
			if setupVault != "" {
				vault = setupVault
			} else if !setupYes && interactive() {
				fmt.Fprintf(os.Stderr, "Ruta del almacén [%s]: ", vault)
				if line := passphrase.ReadLine(); line != "" {
					vault = line
				}
			}
			if v, err := expandPath(vault); err == nil && repoIn(v) {
				fmt.Println("· ese almacén ya tiene datos: este es un SEGUNDO equipo. Se conecta sin crear nada; te pedirá la frase de paso que ya usas.")
			} else {
				fmt.Println("· almacén nuevo: este es el PRIMER equipo. Elige una frase de paso larga: es la única llave de todo.")
			}
			if err := runInit(cfg, vault); err != nil {
				return err
			}
			if cfg, err = config.Load(); err != nil {
				return err
			}
		}

		// ── 3. imágenes ──
		paso(3, "Imágenes de Flowsint")
		cacheDir, err := dock.CacheDir()
		if err != nil {
			return err
		}
		images := dock.Images(cfg.FlowsintVersion)
		faltan := 0
		for _, img := range images {
			if !dock.InCache(cacheDir, img) {
				faltan++
			}
		}
		switch {
		case faltan == 0:
			fmt.Println("✔ las", len(images), "imágenes ya están en la caché")
		case askYN(fmt.Sprintf("Faltan %d imágenes (varios GiB). ¿Descargarlas ahora? Si no, lo hará el primer `up`", faltan), true):
			// primero las exactas del almacén (si las anota), luego el resto
			if err := imagesSync(cfg, cacheDir, images); err != nil {
				fmt.Println("· no se han podido igualar por digest (" + err.Error() + "); se descargan por etiqueta")
			}
			if err := imagesPull(cacheDir, images); err != nil {
				return err
			}
		default:
			fmt.Println("· se descargarán en el primer `up`")
		}

		// ── 4. prueba ──
		paso(4, "Prueba de humo")
		if askYN("¿Probar ahora un ciclo completo (arrancar, guardar, cerrar, restaurar) con un almacén desechable? Tarda unos minutos y no toca tus datos", nuevoEquipo) {
			if err := runSelftest(); err != nil {
				return fmt.Errorf("la prueba de humo ha fallado: %w", err)
			}
		} else {
			fmt.Println("· omitida (puedes lanzarla cuando quieras con `flowsint-desk selftest`)")
		}

		// ── 5. arranque ──
		paso(5, "Listo")
		fmt.Println("✔ este equipo está preparado.")
		fmt.Println("  Uso diario:  flowsint-desk up   (Ctrl+C guarda y cierra)   ·   flowsint-desk backup   ·   flowsint-desk status")
		if askYN("¿Arrancar Flowsint ahora?", false) {
			return upCmd.RunE(upCmd, nil)
		}
		return nil
	},
}

// repoIn: hay un repositorio restic en ese directorio.
func repoIn(vault string) bool {
	st, err := os.Stat(filepath.Join(vault, "config"))
	return err == nil && !st.IsDir()
}

func init() {
	setupCmd.Flags().BoolVar(&setupYes, "yes", false, "no preguntar: aceptar los valores por defecto")
	setupCmd.Flags().StringVar(&setupEngine, "engine", "", "motor de contenedores: docker o podman (si no, se pregunta la primera vez)")
	setupCmd.Flags().StringVar(&setupVault, "vault", "", "ruta del almacén (si no, se propone dentro del pCloud Drive detectado)")
}
