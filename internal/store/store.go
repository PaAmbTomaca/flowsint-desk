// Package store es el wrapper de restic. La frase de paso NUNCA va en argv:
// viaja por RESTIC_PASSWORD_FILE apuntando a un fichero 0600 en tmpfs.
package store

import (
	"encoding/json"
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

func run(e Env, args ...string) (string, error) {
	cmd := exec.Command("restic", args...)
	cmd.Env = e.env()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Exists comprueba que el repo existe y la frase es correcta (snapshots OK).
func Exists(e Env) (bool, error) {
	out, err := run(e, "snapshots", "--json")
	if err != nil {
		if strings.Contains(out, "wrong password") || strings.Contains(out, "incorrect password") || strings.Contains(out, "Wrong password") {
			return false, fmt.Errorf("frase de paso INCORRECTA para el repositorio %s", e.Repo)
		}
		return false, fmt.Errorf("restic snapshots: %v: %s", err, out)
	}
	return true, nil
}

// Init crea el repositorio si no existe.
func Init(e Env) (string, error) {
	out, err := run(e, "init")
	if err != nil {
		if strings.Contains(out, "already initialized") || strings.Contains(out, "already exists") {
			return "ya existía", nil
		}
		return out, err
	}
	return strings.TrimSpace(out), nil
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
	Summary SnapSummary `json:"summary"`
}

// Snapshots lista todos los snapshots, ordenados por fecha.
func Snapshots(e Env) ([]Snap, error) {
	out, err := run(e, "snapshots", "--json")
	if err != nil {
		return nil, fmt.Errorf("restic snapshots: %v: %s", err, out)
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
	out, err := run(e, args...)
	if err != nil {
		return "", fmt.Errorf("restic backup: %v: %s", err, out)
	}
	var res struct {
		SnapshotID string `json:"snapshot_id"`
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		return "", fmt.Errorf("parseando backup: %w (salida: %.200s)", err, out)
	}
	return res.SnapshotID, nil
}

// Restore restaura un snapshot a target.
func Restore(e Env, id, target string) error {
	out, err := run(e, "restore", id, "--target", target)
	if err != nil {
		return fmt.Errorf("restic restore %s: %v: %s", id, err, out)
	}
	return nil
}

// Ls lista los paths dentro de un snapshot.
func Ls(e Env, id string) ([]string, error) {
	out, err := run(e, "ls", id, "--json")
	if err != nil {
		return nil, fmt.Errorf("restic ls %s: %v: %s", id, err, out)
	}
	var res struct {
		StructType string `json:"struct_type"`
	}
	_ = json.Unmarshal([]byte(out), &res)
	var entries []struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(out), &entries); err == nil && len(entries) > 0 {
		paths := make([]string, 0, len(entries))
		for _, e := range entries {
			paths = append(paths, e.Path)
		}
		return paths, nil
	}
	// formato plano: una línea JSON por entrada
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
	out, err := run(e, "dump", id, path)
	if err != nil {
		return "", fmt.Errorf("restic dump %s %s: %v: %s", id, path, err, out)
	}
	return out, nil
}

// Forget aplica retención y (opcional) prune.
func Forget(e Env, keepHourly, keepDaily, keepMonthly int, prune bool) (string, error) {
	args := []string{"forget",
		fmt.Sprintf("--keep-hourly=%d", keepHourly),
		fmt.Sprintf("--keep-daily=%d", keepDaily),
		fmt.Sprintf("--keep-monthly=%d", keepMonthly),
	}
	if prune {
		args = append(args, "--prune")
	}
	out, err := run(e, args...)
	if err != nil {
		return out, fmt.Errorf("restic forget: %v: %s", err, out)
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
	out, err := run(e, "key", "list", "--json")
	if err != nil {
		return nil, fmt.Errorf("restic key list: %v: %s", err, out)
	}
	var keys []Key
	if err := json.Unmarshal([]byte(out), &keys); err != nil {
		return nil, err
	}
	return keys, nil
}

// KeyAdd añade una clave nueva con la frase de newPassFile (fichero 0600 tmpfs).
func KeyAdd(e Env, newPassFile string) (string, error) {
	out, err := run(e, "key", "add", "--new-password-file", newPassFile, "--json")
	if err != nil {
		return "", fmt.Errorf("restic key add: %v: %s", err, out)
	}
	var res struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &res); err == nil && res.ID != "" {
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
	out, err := run(e, "key", "remove", id)
	if err != nil {
		return fmt.Errorf("restic key remove %s: %v: %s", id, err, out)
	}
	return nil
}
