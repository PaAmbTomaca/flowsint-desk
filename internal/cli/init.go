package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"flowsint-desk/internal/config"
	"flowsint-desk/internal/passphrase"
	"flowsint-desk/internal/run"
	"flowsint-desk/internal/secrets"
	"flowsint-desk/internal/snap"
	"flowsint-desk/internal/store"

	"github.com/spf13/cobra"
)

var initVault string

// expandPath resuelve $VARS y un ~ inicial (en el prompt no hay shell que lo
// expanda) y devuelve una ruta absoluta.
func expandPath(p string) (string, error) {
	p = os.ExpandEnv(strings.TrimSpace(p))
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return filepath.Abs(p)
}

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Configura el almacén (pCloud); crea el repo restic y el primer snapshot, o se conecta al que ya existe",
	Long: `En el primer equipo crea el repositorio restic en el almacén, genera los
secretos de Flowsint y guarda un primer snapshot vacío.

En un segundo equipo (el almacén ya tiene repositorio y snapshots) NO crea
nada: solo guarda la ruta en la configuración, comprueba la frase de paso y
ofrece recordarla en el llavero. Los datos se recuperan con "up".`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		// ruta del almacén
		vault := initVault
		if vault == "" {
			vault = cfg.VaultPath
			fmt.Fprintf(os.Stderr, "Ruta del almacén (pCloud) [%s]: ", vault)
			if line := passphrase.ReadLine(); line != "" {
				vault = line
			}
		}
		if vault, err = expandPath(vault); err != nil {
			return err
		}
		if err := os.MkdirAll(vault, 0o700); err != nil {
			return fmt.Errorf("creando almacén %s: %w", vault, err)
		}
		cfg.VaultPath = vault
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Println("✔ configuración guardada en", cfg.Path, "(sin secretos)")

		runDir, err := run.Ensure()
		if err != nil {
			return err
		}
		passFile := filepath.Join(runDir, "restic-pass")
		se := store.Env{Repo: vault, PassFile: passFile, CacheDir: filepath.Join(runDir, "restic-cache")}
		host, _ := os.Hostname()
		boot, _ := run.BootID()

		// ¿el almacén ya tiene repositorio? (segundo equipo, o init repetido)
		existente := false
		if st, err := os.Stat(filepath.Join(vault, "config")); err == nil && !st.IsDir() {
			existente = true
		}
		var pass string
		if existente {
			fmt.Println("· el almacén ya tiene un repositorio restic: no se crea uno nuevo")
			if pass, err = passphrase.Prompt("Frase de paso del almacén existente", 1); err != nil {
				return err
			}
		} else {
			if pass, err = passphrase.PromptTwice(passphrase.MinLen); err != nil {
				return err
			}
		}
		if err := os.WriteFile(passFile, []byte(pass), 0o600); err != nil {
			return err
		}
		// restic-pass es solo para este proceso: no sobrevive a init, acabe
		// bien o mal (la copia `passphrase` solo se escribe al final, si todo fue bien)
		defer os.Remove(passFile)

		if existente {
			if _, err := store.Exists(se); err != nil {
				return fmt.Errorf("no se pudo abrir el almacén existente: %w", err)
			}
			snaps, err := store.Snapshots(se)
			if err != nil {
				return err
			}
			if len(snaps) > 0 {
				// NUNCA un snapshot "init" nuevo encima de datos: `up` restaura el
				// último snapshot, y uno vacío con secretos nuevos taparía todo.
				last := snaps[len(snaps)-1]
				fmt.Printf("✔ frase correcta · %d snapshots en el almacén (último: %s, %s, host %s)\n",
					len(snaps), last.ShortID, last.Time.Local().Format("2006-01-02 15:04"), last.Host)
				if err := finishInit(runDir, pass); err != nil {
					return err
				}
				fmt.Println("✔ equipo conectado al almacén existente (no se ha creado ni modificado nada en él). Siguiente paso: `flowsint-desk up`")
				return nil
			}
			fmt.Println("· el repositorio existe pero no tiene snapshots: se completa el init")
		} else {
			creado, err := store.Init(se)
			if err != nil {
				return err
			}
			if !creado {
				return fmt.Errorf("restic dice que en %s ya hay un repositorio aunque no se ve su fichero config: revisa que pCloud haya terminado de sincronizar y repite `flowsint-desk init`", vault)
			}
			fmt.Println("✔ repositorio restic creado en", vault)
		}

		// secretos internos en tmpfs (viajarán cifrados dentro del snapshot)
		sec, err := secrets.Generate()
		if err != nil {
			return err
		}
		secFile := filepath.Join(runDir, "secrets.env")
		if err := secrets.WriteEnv(secFile, sec); err != nil {
			return err
		}
		fmt.Println("✔ secretos de Flowsint generados en tmpfs (0600)")

		// primer snapshot vacío (secrets.env + manifest init)
		staging := filepath.Join(runDir, "staging")
		if err := os.RemoveAll(staging); err != nil {
			return err
		}
		if err := os.MkdirAll(staging, 0o700); err != nil {
			return err
		}
		secB, err := os.ReadFile(secFile)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(staging, "secrets.env"), secB, 0o600); err != nil {
			return err
		}
		h, err := snap.SHA256File(filepath.Join(staging, "secrets.env"))
		if err != nil {
			return err
		}
		m := snap.Manifest{
			Version: snap.Version, Kind: "init",
			Created: time.Now().UTC().Format(time.RFC3339),
			Host:    host, BootID: boot,
			SHA256: map[string]string{"secrets.env": h},
		}
		mb, _ := json.MarshalIndent(m, "", "  ")
		if err := os.WriteFile(filepath.Join(staging, "manifest.json"), mb, 0o600); err != nil {
			return err
		}
		id, err := store.Backup(se, staging, map[string]string{"host": host, "boot": short(boot), "tipo": "init"})
		if err != nil {
			return err
		}
		fmt.Printf("✔ primer snapshot creado: %s (vacío: solo secretos)\n", short(id))
		if err := finishInit(runDir, pass); err != nil {
			return err
		}
		fmt.Println("✔ init listo. Siguiente paso: `flowsint-desk up`")
		return nil
	},
}

// finishInit deja la frase en tmpfs para que el primer `up` de este arranque
// no la vuelva a pedir (se borra con el directorio de ejecución al cerrar la
// sesión o al apagar) y ofrece recordarla en el llavero.
func finishInit(runDir, pass string) error {
	if err := os.WriteFile(filepath.Join(runDir, "passphrase"), []byte(pass), 0o600); err != nil {
		return err
	}
	fmt.Println("✔ frase de paso en tmpfs (0600) hasta el próximo `up`/apagado")
	if save, _ := passphrase.AskSaveKeyring(); save {
		if err := passphrase.SaveKeyring(pass); err != nil {
			fmt.Fprintln(os.Stderr, "aviso: no se pudo guardar en el llavero:", err)
		} else {
			fmt.Println("✔ frase recordada en el llavero del sistema (Secret Service)")
		}
	}
	return nil
}

func init() { initCmd.Flags().StringVar(&initVault, "vault", "", "ruta del almacén (pCloud)") }
