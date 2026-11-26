package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"flowsint-desk/internal/config"
	"flowsint-desk/internal/passphrase"
	"flowsint-desk/internal/run"
	"flowsint-desk/internal/session"
	"flowsint-desk/internal/store"
)

// short recorta un id (snapshot, boot_id, clave) a 8 caracteres para
// mostrarlo, sin reventar si viene más corto o vacío.
func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

const origenPrompt = "prompt"

// readPass devuelve la frase de paso y de dónde salió, en esta prioridad:
// 1) <runDir>/passphrase (la dejó `init` en tmpfs), 2) llavero, 3) prompt.
// Con soloPrompt se salta 1 y 2 (cuando la guardada resultó no valer).
func readPass(runDir string, soloPrompt bool) (pass, origen string, err error) {
	if !soloPrompt {
		if b, err := os.ReadFile(filepath.Join(runDir, "passphrase")); err == nil && len(b) > 0 {
			return string(b), "el fichero passphrase de tmpfs", nil
		}
		if p, err := passphrase.FromKeyring(); err == nil {
			return p, "el llavero", nil
		}
	}
	// repo ya existente: la longitud mínima la impuso init/passwd en su día
	p, err := passphrase.Prompt("Frase de paso", 1)
	return p, origenPrompt, err
}

// unlockStore resuelve la frase, la deja en passFile (0600, tmpfs) para
// restic y comprueba que abre el repositorio. Si la frase guardada (fichero
// de init o llavero) NO abre este almacén —llavero con la frase de otro
// almacén, o desfasado tras un passwd en el otro equipo— la pide por
// terminal en vez de rendirse. Si falla, no deja la frase mala en passFile.
func unlockStore(cfg *config.Config, runDir, passFile string) (store.Env, error) {
	cacheDir := filepath.Join(runDir, "restic-cache")
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return store.Env{}, err
	}
	se := store.Env{Repo: cfg.VaultPath, PassFile: passFile, CacheDir: cacheDir}
	pass, origen, err := readPass(runDir, false)
	if err != nil {
		return se, err
	}
	for {
		if err := os.WriteFile(passFile, []byte(pass), 0o600); err != nil {
			return se, err
		}
		ok, err := store.Exists(se)
		if err == nil && ok {
			return se, nil
		}
		_ = os.Remove(passFile)
		if err == nil {
			return se, fmt.Errorf("el almacén %s no tiene repositorio restic todavía: ejecuta `flowsint-desk init`", cfg.VaultPath)
		}
		if errors.Is(err, store.ErrWrongPassword) && origen != origenPrompt {
			fmt.Fprintf(os.Stderr, "aviso: la frase guardada en %s no abre este almacén; tecléala\n", origen)
			if pass, origen, err = readPass(runDir, true); err != nil {
				return se, err
			}
			continue
		}
		return se, fmt.Errorf("no se pudo desbloquear el almacén: %w", err)
	}
}

// mustStoreEnv desbloquea el almacén para `up`, que es el dueño de
// <runDir>/restic-pass durante toda la sesión. Frase incorrecta → error
// ANTES de tocar nada más.
func mustStoreEnv(cfg *config.Config, runDir string) (store.Env, error) {
	return unlockStore(cfg, runDir, filepath.Join(runDir, "restic-pass"))
}

// openStore desbloquea el almacén para los comandos que NO son `up`
// (status, snapshots, backup, forget, passwd). Devuelve una función de
// limpieza que hay que llamar siempre.
//
//   - Con una sesión viva reutiliza su restic-pass sin reescribirlo: antes,
//     teclear mal la frase en `status`/`backup` pisaba el fichero de la sesión
//     y a partir de ahí fallaban todos sus snapshots, incluido el final.
//   - Sin sesión usa un fichero propio del proceso y lo borra al terminar: la
//     frase no se queda en /tmp hasta el siguiente reinicio.
func openStore(cfg *config.Config, runDir string) (store.Env, func(), error) {
	nada := func() {}
	sesPass := filepath.Join(runDir, "restic-pass")
	if s, err := session.Read(runDir); err == nil && s.Alive() {
		if _, err := os.Stat(sesPass); err == nil {
			se := store.Env{Repo: cfg.VaultPath, PassFile: sesPass, CacheDir: filepath.Join(runDir, "restic-cache")}
			if _, err := store.Exists(se); err != nil {
				return se, nada, fmt.Errorf("no se pudo leer el almacén con la frase de la sesión viva: %w", err)
			}
			return se, nada, nil
		}
	}
	propio := filepath.Join(runDir, fmt.Sprintf("restic-pass.%d", os.Getpid()))
	limpiar := func() {
		_ = os.Remove(propio)
		tidyRunDir(runDir)
	}
	se, err := unlockStore(cfg, runDir, propio)
	if err != nil {
		limpiar()
		return se, nada, err
	}
	return se, limpiar, nil
}

// tidyRunDir borra el directorio de ejecución si lo único que contiene es lo
// que deja un comando de consulta (subdirectorios vacíos y la caché de
// restic, que es metadato cifrado del repo). No toca nada si hay sesión, ni
// si queda cualquier otra cosa: la frase que `init` deja para el primer `up`,
// secretos, un socket o restos de una sesión.
func tidyRunDir(runDir string) {
	if s, err := session.Read(runDir); err == nil && s.Alive() {
		return
	}
	entries, err := os.ReadDir(runDir)
	if err != nil {
		return
	}
	vaciables := map[string]bool{"rk-state": true, "dockerd-data": true, "dockerd-exec": true, "stack": true,
		"neo4j-import": true, "staging": true, "restore": true}
	for _, e := range entries {
		switch {
		case e.Name() == "restic-cache":
		case e.IsDir() && vaciables[e.Name()]:
			if sub, err := os.ReadDir(filepath.Join(runDir, e.Name())); err != nil || len(sub) > 0 {
				return
			}
		default:
			return
		}
	}
	_ = run.Cleanup(runDir)
}
