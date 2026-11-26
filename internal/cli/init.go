package cli

import (
	"bufio"
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

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Configura el almacén (pCloud), crea el repo restic y el primer snapshot",
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
			line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if line = strings.TrimSpace(line); line != "" {
				vault = line
			}
		}
		vault = os.ExpandEnv(vault)
		if err := os.MkdirAll(vault, 0o700); err != nil {
			return fmt.Errorf("creando almacén %s: %w", vault, err)
		}
		cfg.VaultPath = vault
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Println("✔ configuración guardada en", cfg.Path, "(sin secretos)")

		// frase de paso (dos veces)
		pass, err := passphrase.PromptTwice(8)
		if err != nil {
			return err
		}
		runDir, err := run.Ensure()
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(runDir, "passphrase"), []byte(pass), 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(runDir, "restic-pass"), []byte(pass), 0o600); err != nil {
			return err
		}
		fmt.Println("✔ frase de paso guardada en tmpfs (0600)")

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

		// repo restic + primer snapshot vacío (secrets.env + manifest init)
		se := store.Env{Repo: vault, PassFile: filepath.Join(runDir, "restic-pass"), CacheDir: filepath.Join(runDir, "restic-cache")}
		creado, err := store.Init(se)
		if err != nil {
			return err
		}
		if creado {
			fmt.Println("✔ repositorio restic creado")
		} else {
			fmt.Println("✔ repositorio restic: ya existía")
		}
		staging := filepath.Join(runDir, "staging")
		if err := os.MkdirAll(staging, 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(staging, "secrets.env"), mustRead(secFile), 0o600); err != nil {
			return err
		}
		host, _ := os.Hostname()
		boot, _ := run.BootID()
		h, _ := snap.SHA256File(filepath.Join(staging, "secrets.env"))
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

		if save, _ := passphrase.AskSaveKeyring(); save {
			if err := passphrase.SaveKeyring(pass); err != nil {
				fmt.Fprintln(os.Stderr, "aviso: no se pudo guardar en el llavero:", err)
			} else {
				fmt.Println("✔ frase recordada en el llavero del sistema (Secret Service)")
			}
		}
		fmt.Println("✔ init listo. Siguientes pasos: `flowsint-desk up`")
		return nil
	},
}

func mustRead(p string) []byte {
	b, err := os.ReadFile(p)
	if err != nil {
		panic(err)
	}
	return b
}

func init() { initCmd.Flags().StringVar(&initVault, "vault", "", "ruta del almacén (pCloud)") }
