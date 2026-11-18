package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"flowsint-desk/internal/config"
	"flowsint-desk/internal/passphrase"
	"flowsint-desk/internal/run"
	"flowsint-desk/internal/store"

	"github.com/spf13/cobra"
)

var passwdCmd = &cobra.Command{
	Use:   "passwd",
	Short: "Cambia la frase de paso (restic key add + quitar la clave antigua)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		runDir, err := run.Ensure()
		if err != nil {
			return err
		}
		// frase actual (valida contra el repo antes de cambiar nada)
		se, err := mustStoreEnv(cfg, runDir)
		if err != nil {
			return err
		}
		fmt.Println("✔ frase actual correcta")
		nueva, err := passphrase.PromptTwice(8)
		if err != nil {
			return err
		}
		newFile := filepath.Join(runDir, "restic-pass-new")
		if err := os.WriteFile(newFile, []byte(nueva), 0o600); err != nil {
			return err
		}
		defer os.Remove(newFile)
		newID, err := store.KeyAdd(se, newFile)
		if err != nil {
			return err
		}
		fmt.Println("✔ clave nueva añadida:", newID[:8])
		// OJO: quitar claves hay que hacerlo autenticándose con la clave NUEVA;
		// restic se niega a quitar la clave que usa la sesión actual.
		seNew := store.Env{Repo: se.Repo, PassFile: newFile, CacheDir: se.CacheDir}
		keys, err := store.KeyList(seNew)
		if err != nil {
			return err
		}
		for _, k := range keys {
			if k.ID != newID {
				if err := store.KeyRemove(seNew, k.ID); err != nil {
					return fmt.Errorf("no se pudo quitar la clave antigua %s: %w", k.ID[:8], err)
				}
				fmt.Println("✔ clave antigua eliminada:", k.ID[:8])
			}
		}
		// actualizar copias locales de la frase
		if err := os.WriteFile(filepath.Join(runDir, "passphrase"), []byte(nueva), 0o600); err != nil {
			return err
		}
		if err := os.WriteFile(se.PassFile, []byte(nueva), 0o600); err != nil {
			return err
		}
		if _, err := passphrase.FromKeyring(); err == nil {
			if save, _ := passphrase.AskSaveKeyring(); save {
				_ = passphrase.SaveKeyring(nueva)
				fmt.Println("✔ llavero actualizado")
			}
		}
		// comprobar que la nueva frase abre el repo
		se2 := store.Env{Repo: cfg.VaultPath, PassFile: newFile, CacheDir: filepath.Join(runDir, "restic-cache")}
		if _, err := store.Exists(se2); err != nil {
			return fmt.Errorf("comprobación final falló: %w", err)
		}
		fmt.Println("✔ frase cambiada y verificada contra el repositorio")
		return nil
	},
}
