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
		se, limpiar, err := openStore(cfg, runDir)
		if err != nil {
			return err
		}
		defer limpiar()
		fmt.Println("✔ frase actual correcta")
		nueva, err := passphrase.PromptTwice(passphrase.MinLen)
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
		fmt.Println("✔ clave nueva añadida:", short(newID))
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
					return fmt.Errorf("no se pudo quitar la clave antigua %s: %w", short(k.ID), err)
				}
				fmt.Println("✔ clave antigua eliminada:", short(k.ID))
			}
		}
		// actualizar las copias locales de la frase QUE YA EXISTAN (no crear nuevas)
		if pf := filepath.Join(runDir, "passphrase"); fileExists(pf) {
			if err := os.WriteFile(pf, []byte(nueva), 0o600); err != nil {
				return err
			}
		}
		if err := os.WriteFile(se.PassFile, []byte(nueva), 0o600); err != nil {
			return err
		}
		// llavero: o se actualiza o se borra; dejar la frase vieja haría que el
		// siguiente `up` intente abrir el almacén con una frase que ya no vale
		if _, err := passphrase.FromKeyring(); err == nil {
			if save, _ := passphrase.AskSaveKeyring(); save {
				if err := passphrase.SaveKeyring(nueva); err != nil {
					fmt.Fprintln(os.Stderr, "aviso: no se pudo actualizar el llavero:", err)
				} else {
					fmt.Println("✔ llavero actualizado")
				}
			} else if err := passphrase.DeleteKeyring(); err != nil {
				fmt.Fprintln(os.Stderr, "aviso: el llavero conserva la frase ANTIGUA y no se pudo borrar:", err)
			} else {
				fmt.Println("✔ frase antigua borrada del llavero (se pedirá por terminal)")
			}
		}
		fmt.Println("· en tus otros equipos, la frase guardada en el llavero ya no vale: `up` te la pedirá por terminal")
		// comprobar que la nueva frase abre el repo
		se2 := store.Env{Repo: cfg.VaultPath, PassFile: newFile, CacheDir: filepath.Join(runDir, "restic-cache")}
		if _, err := store.Exists(se2); err != nil {
			return fmt.Errorf("comprobación final falló: %w", err)
		}
		fmt.Println("✔ frase cambiada y verificada contra el repositorio")
		return nil
	},
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
