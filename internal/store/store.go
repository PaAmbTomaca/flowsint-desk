// Package store es el wrapper de restic. La frase de paso NUNCA va en argv:
// viaja por RESTIC_PASSWORD_FILE apuntando a un fichero 0600 en tmpfs.
package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Env identifica el repositorio, el fichero de frase y la caché local.
// La caché de restic va a tmpfs (run dir): en disco solo deben quedar la
// configuración y la caché de imágenes públicas.
type Env struct {
	Repo     string
	PassFile string
	CacheDir string
}

func (e Env) env() []string {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".local", "bin") + string(os.PathListSeparator) + os.Getenv("PATH")
	env := []string{
		"RESTIC_REPOSITORY=" + e.Repo,
		"RESTIC_PASSWORD_FILE=" + e.PassFile,
		"PATH=" + path,
		"HOME=" + home,
	}
	if e.CacheDir != "" {
		env = append(env, "RESTIC_CACHE_DIR="+e.CacheDir)
	}
	return env
}

// Errores tipados para que la CLI pueda reaccionar (p. ej. volver a pedir la
// frase si la del llavero ya no vale) sin depender de comparar textos.
var (
	ErrWrongPassword = errors.New("frase de paso INCORRECTA")
	ErrNoRepo        = errors.New("no hay repositorio restic")
)

// Códigos de salida de restic (≥ 0.17): 10 = el repositorio no existe,
// 12 = contraseña incorrecta.
const (
	exitNoRepo        = 10
	exitWrongPassword = 12
)

// run ejecuta restic y devuelve stdout y stderr POR SEPARADO: los avisos que
// restic escribe en stderr (caché, bloqueos antiguos, reintentos sobre FUSE)
// no deben mezclarse con el JSON de stdout, o el parseo falla aunque la
// operación haya ido bien.
func run(e Env, args ...string) (stdout, stderr string, err error) {
	cmd := exec.Command("restic", args...)
	cmd.Env = e.env()
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	err = cmd.Run()
	return so.String(), se.String(), err
}

// classify traduce un fallo de restic a los errores tipados (por código de
// salida y, como red de seguridad para versiones viejas, por el texto).
func classify(err error, stderr string) error {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		switch ee.ExitCode() {
		case exitWrongPassword:
			return ErrWrongPassword
		case exitNoRepo:
			return ErrNoRepo
		}
	}
	low := strings.ToLower(stderr)
	switch {
	case strings.Contains(low, "wrong password"), strings.Contains(low, "incorrect password"):
		return ErrWrongPassword
	case strings.Contains(low, "is there a repository at the following location"),
		strings.Contains(low, "repository does not exist"):
		return ErrNoRepo
	}
	return nil
}

// Exists comprueba que el repo existe y la frase es correcta (snapshots OK).
// Devuelve (false, nil) si no hay repositorio, y ErrWrongPassword (envuelto)
// si la frase no abre el repo.
func Exists(e Env) (bool, error) {
	_, stderr, err := run(e, "snapshots", "--json")
	if err != nil {
		switch classify(err, stderr) {
		case ErrWrongPassword:
			return false, fmt.Errorf("%w para el repositorio %s", ErrWrongPassword, e.Repo)
		case ErrNoRepo:
			return false, nil
		}
		return false, fmt.Errorf("restic snapshots: %v: %s", err, strings.TrimSpace(stderr))
	}
	return true, nil
}

// Init crea el repositorio. Devuelve creado=false si ya existía (sin tocarlo).
func Init(e Env) (creado bool, err error) {
	_, stderr, err := run(e, "init")
	if err != nil {
		if strings.Contains(stderr, "already initialized") || strings.Contains(stderr, "already exists") {
			return false, nil
		}
		return false, fmt.Errorf("restic init: %v: %s", err, strings.TrimSpace(stderr))
	}
	return true, nil
}

type SnapSummary struct {
	DataAdded           int64 `json:"data_added"`
	TotalSizeProcessed  int64 `json:"total_size_processed"`
	TotalFilesProcessed int   `json:"total_files_processed"`
}

type Snap struct {
	ID      string      `json:"id"`
	ShortID string      `json:"short_id"`
	Time    time.Time   `json:"time"`
	Host    string      `json:"hostname"`
	Tags    []string    `json:"tags"`
	Paths   []string    `json:"paths"`
	Summary SnapSummary `json:"summary"`
}

// Snapshots lista todos los snapshots, ordenados por fecha.
func Snapshots(e Env) ([]Snap, error) {
	out, stderr, err := run(e, "snapshots", "--json")
	if err != nil {
		return nil, fmt.Errorf("restic snapshots: %v: %s", err, strings.TrimSpace(stderr))
	}
	var snaps []Snap
	if err := json.Unmarshal([]byte(out), &snaps); err != nil {
		return nil, fmt.Errorf("parseando snapshots: %w", err)
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Time.Before(snaps[j].Time) })
	return snaps, nil
}

// Latest devuelve el snapshot más reciente (o "" si no hay).
func Latest(e Env) (string, error) {
	snaps, err := Snapshots(e)
	if err != nil {
		return "", err
	}
	if len(snaps) == 0 {
		return "", nil
	}
	return snaps[len(snaps)-1].ID, nil
}

// Backup crea un snapshot del directorio dir y devuelve su ID.
func Backup(e Env, dir string, tags map[string]string) (string, error) {
	args := []string{"backup", "--json", dir}
	keys := make([]string, 0, len(tags))
	for k := range tags {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--tag", k+"="+tags[k])
	}
	out, stderr, err := run(e, args...)
	if err != nil {
		return "", fmt.Errorf("restic backup: %v: %s", err, strings.TrimSpace(stderr))
	}
	id := parseBackupID(out)
	if id == "" {
		return "", fmt.Errorf("restic backup terminó sin resumen (sin snapshot_id; salida: %.200s)", out)
	}
	return id, nil
}

// parseBackupID extrae el snapshot_id de la salida de `restic backup --json`,
// que es JSON Lines: N mensajes de progreso ("status") y al final un
// "summary". Se recorre línea a línea y se ignora lo que no sea el resumen.
func parseBackupID(out string) string {
	id := ""
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var msg struct {
			MessageType string `json:"message_type"`
			SnapshotID  string `json:"snapshot_id"`
		}
		if json.Unmarshal([]byte(line), &msg) == nil && msg.MessageType == "summary" && msg.SnapshotID != "" {
			id = msg.SnapshotID
		}
	}
	return id
}

// Restore restaura un snapshot a target.
func Restore(e Env, id, target string) error {
	_, stderr, err := run(e, "restore", id, "--target", target)
	if err != nil {
		return fmt.Errorf("restic restore %s: %v: %s", id, err, strings.TrimSpace(stderr))
	}
	return nil
}

// Ls lista los paths dentro de un snapshot (`restic ls --json` es JSON Lines:
// una cabecera del snapshot y luego una línea por entrada).
func Ls(e Env, id string) ([]string, error) {
	out, stderr, err := run(e, "ls", id, "--json")
	if err != nil {
		return nil, fmt.Errorf("restic ls %s: %v: %s", id, err, strings.TrimSpace(stderr))
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var e struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(line), &e) == nil && e.Path != "" {
			paths = append(paths, e.Path)
		}
	}
	return paths, nil
}

// Dump imprime un fichero de dentro de un snapshot a stdout (para leer manifest.json).
func Dump(e Env, id, path string) (string, error) {
	out, stderr, err := run(e, "dump", id, path)
	if err != nil {
		return "", fmt.Errorf("restic dump %s %s: %v: %s", id, path, err, strings.TrimSpace(stderr))
	}
	return out, nil
}

// Forget aplica retención y (opcional) prune.
func Forget(e Env, keepHourly, keepDaily, keepMonthly int, prune bool) (string, error) {
	args := []string{"forget",
		// Sin agrupar: restic agrupa por host+paths por defecto y la ruta del
		// staging lleva el boot_id, así que cada arranque sería un grupo propio
		// con su propia cuota de retención (el repo no dejaría de crecer).
		// El almacén es UN solo conjunto de datos visto desde varios equipos.
		"--group-by", "",
		fmt.Sprintf("--keep-hourly=%d", keepHourly),
		fmt.Sprintf("--keep-daily=%d", keepDaily),
		fmt.Sprintf("--keep-monthly=%d", keepMonthly),
	}
	if prune {
		args = append(args, "--prune")
	}
	out, stderr, err := run(e, args...)
	if err != nil {
		return out, fmt.Errorf("restic forget: %v: %s", err, strings.TrimSpace(stderr))
	}
	return out, nil
}

type Key struct {
	ID   string `json:"id"`
	User string `json:"user_name"`
	Host string `json:"host_name"`
}

// KeyList lista las claves del repo.
func KeyList(e Env) ([]Key, error) {
	out, stderr, err := run(e, "key", "list", "--json")
	if err != nil {
		return nil, fmt.Errorf("restic key list: %v: %s", err, strings.TrimSpace(stderr))
	}
	var keys []Key
	if err := json.Unmarshal([]byte(out), &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

// KeyAdd añade una clave nueva con la frase de newPassFile (fichero 0600 tmpfs).
func KeyAdd(e Env, newPassFile string) (string, error) {
	stdout, stderr, err := run(e, "key", "add", "--new-password-file", newPassFile, "--json")
	if err != nil {
		return "", fmt.Errorf("restic key add: %v: %s", err, strings.TrimSpace(stderr))
	}
	// según la versión, el mensaje sale por stdout (JSON) o por stderr (texto)
	out := stdout + "\n" + stderr
	var res struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(stdout), &res); err == nil && res.ID != "" {
		return res.ID, nil
	}
	// salida en texto plano de restic: "saved new key with ID <id>"
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "saved new key with ID ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "saved new key with ID ")), nil
		}
	}
	return "", fmt.Errorf("parseando key add (salida: %.200s)", out)
}

// KeyRemove elimina una clave por id.
func KeyRemove(e Env, id string) error {
	_, stderr, err := run(e, "key", "remove", id)
	if err != nil {
		return fmt.Errorf("restic key remove %s: %v: %s", id, err, strings.TrimSpace(stderr))
	}
	return nil
}

// Unlock quita los bloqueos HUÉRFANOS del repositorio restic (los que deja un
// proceso muerto, p. ej. un kill -9 en mitad de un backup). No toca los de
// procesos vivos: es la forma segura de `restic unlock`.
func Unlock(e Env) error {
	_, stderr, err := run(e, "unlock")
	if err != nil {
		return fmt.Errorf("restic unlock: %v: %s", err, strings.TrimSpace(stderr))
	}
	return nil
}

// Check verifica la integridad estructural del repositorio (`restic check`).
func Check(e Env) error {
	stdout, stderr, err := run(e, "check")
	if err != nil {
		return fmt.Errorf("restic check: %v: %s %s", err, strings.TrimSpace(stderr), lastLines(stdout, 5))
	}
	return nil
}

func lastLines(s string, n int) string {
	l := strings.Split(strings.TrimSpace(s), "\n")
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, "\n")
}

// Mirror copia todos los snapshots del repositorio de e a otro repositorio
// restic en dest (segunda copia, misma frase de paso). Si dest no existe lo
// crea con los mismos parámetros de troceado, para que la copia deduplique.
// Es incremental: repetirlo solo copia lo nuevo.
func Mirror(e Env, dest string) (creado bool, err error) {
	d := Env{Repo: dest, PassFile: e.PassFile, CacheDir: e.CacheDir}
	from := []string{"--from-repo", e.Repo, "--from-password-file", e.PassFile}
	if _, serr := os.Stat(filepath.Join(dest, "config")); serr != nil {
		if err := os.MkdirAll(dest, 0o700); err != nil {
			return false, err
		}
		args := append([]string{"init", "--copy-chunker-params"}, from...)
		if _, stderr, err := run(d, args...); err != nil {
			return false, fmt.Errorf("creando la segunda copia en %s: %v: %s", dest, err, strings.TrimSpace(stderr))
		}
		creado = true
	}
	if _, stderr, err := run(d, append([]string{"copy"}, from...)...); err != nil {
		return creado, fmt.Errorf("restic copy a %s: %v: %s", dest, err, strings.TrimSpace(stderr))
	}
	return creado, nil
}

// CountSnapshots: cuántos snapshots hay en el repositorio repo (misma frase).
func CountSnapshots(e Env, repo string) (int, error) {
	s, err := Snapshots(Env{Repo: repo, PassFile: e.PassFile, CacheDir: e.CacheDir})
	return len(s), err
}
