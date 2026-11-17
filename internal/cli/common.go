package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"flowsint-desk/internal/config"
	"flowsint-desk/internal/passphrase"
	"flowsint-desk/internal/run"
	"flowsint-desk/internal/session"
	"flowsint-desk/internal/store"
)

func sessionRead(runDir string) (*session.Session, error) { return session.Read(runDir) }

// syscallKill envía SIGUSR1 (backup inmediato) al pid de la sesión.
func syscallKill(pid int) error { return syscall.Kill(pid, syscall.SIGUSR1) }

var _ = run.Dir

// getPassphrase devuelve la frase de paso en esta prioridad:
// 1) <runDir>/passphrase (la dejó `init` en tmpfs), 2) llavero, 3) prompt.
// La escribe en <runDir>/restic-pass (0600) para restic y devuelve store.Env.
func getPassphrase(cfg *config.Config, runDir string) (store.Env, error) {
	var pass string
	pf := filepath.Join(runDir, "passphrase")
	if b, err := os.ReadFile(pf); err == nil {
		pass = string(b)
	} else if p, err := passphrase.FromKeyring(); err == nil {
		pass = p
	} else {
		p, _, err := passphrase.Get()
		if err != nil {
			return store.Env{}, err
		}
		pass = p
	}
	resticPass := filepath.Join(runDir, "restic-pass")
	if err := os.WriteFile(resticPass, []byte(pass), 0o600); err != nil {
		return store.Env{}, err
	}
	cacheDir := filepath.Join(runDir, "restic-cache")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return store.Env{}, err
	}
	return store.Env{Repo: cfg.VaultPath, PassFile: resticPass, CacheDir: cacheDir}, nil
}

// mustStoreEnv exige repo inicializado y frase correcta; si la frase es
// incorrecta, devuelve error ANTES de tocar nada más.
func mustStoreEnv(cfg *config.Config, runDir string) (store.Env, error) {
	se, err := getPassphrase(cfg, runDir)
	if err != nil {
		return se, err
	}
	ok, err := store.Exists(se)
	if err != nil {
		return se, fmt.Errorf("no se pudo desbloquear el almacén: %w", err)
	}
	if !ok {
		return se, fmt.Errorf("el almacén %s no tiene repositorio restic todavía: ejecuta `flowsint-desk init`", cfg.VaultPath)
	}
	return se, nil
}
