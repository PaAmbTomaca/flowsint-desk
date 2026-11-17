// Package dock gestiona el Docker rootless efímero de la sesión:
// arranque/parada del daemon con data-root en tmpfs, caché de imágenes en
// disco (no contienen datos de usuario) y ejecución de `docker compose`.
// NUNCA usa el socket rootful del sistema: siempre DOCKER_HOST al socket
// rootless del directorio de ejecución.
package dock

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"flowsint-desk/internal/run"
)

// Images son las imágenes oficiales de Flowsint y sus dependencias.
// alpine:3 se usa solo para limpiar el data-root desde dentro del user-ns.
var Images = []string{
	"alpine:3",
	"postgres:15-alpine",
	"redis:7-alpine",
	"neo4j:5",
	"ghcr.io/reconurge/flowsint-api:latest",
	"ghcr.io/reconurge/flowsint-app:latest",
}

// WipeSubdirs borra subdirectorios de parent DENTRO del user-namespace del
// daemon rootless (un único contenedor), porque desde el host los ficheros del
// data-root pertenecen a uids mapeados (subuid) y rm -rf falla con
// "Permiso denegado". El daemon debe estar vivo. Se hace en UNA sola ejecución:
// borrar el data-root por partes corrompe el daemon vivo.
func WipeSubdirs(env []string, parent string, subs ...string) error {
	args := []string{"rm", "-rf"}
	for _, s := range subs {
		args = append(args, "/wipe/"+s)
	}
	args = append(args, "2>/dev/null", "||", "true")
	out, err := Run(env, "docker", "run", "--rm",
		"-v", parent+":/wipe",
		"--entrypoint", "sh", "alpine:3", "-c", strings.Join(args, " "))
	if err != nil {
		return fmt.Errorf("wipe de %v en %s: %v: %s", subs, parent, err, out)
	}
	return nil
}

// Env devuelve el entorno para invocar docker apuntando SOLO al rootless.
func Env(runDir string) []string {
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".local", "bin") + string(os.PathListSeparator) + os.Getenv("PATH")
	env := []string{
		"DOCKER_HOST=unix://" + run.Socket(runDir),
		"PATH=" + path,
		"HOME=" + home,
		"USER=" + os.Getenv("USER"),
		"LOGNAME=" + os.Getenv("LOGNAME"),
		"SHELL=" + os.Getenv("SHELL"),
	}
	if v := os.Getenv("XDG_RUNTIME_DIR"); v != "" {
		env = append(env, "XDG_RUNTIME_DIR="+v)
	}
	return env
}

// Run ejecuta un comando con el entorno rootless y devuelve stdout+stderr.
func Run(env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// CP copia entre host y contenedor con `docker cp` (el daemon escribe como
// root del user-ns; escribir directo desde el host deja ficheros "nobody"
// dentro del contenedor rootless y el proceso del contenedor no los lee).
func CP(env []string, args ...string) error {
	out, err := Run(env, "docker", append([]string{"cp"}, args...)...)
	if err != nil {
		return fmt.Errorf("docker cp %v: %v: %.200s", args, err, out)
	}
	return nil
}

// ExecStdin ejecuta un comando con stdin tomado de r (p. ej. psql < dump.sql).
func ExecStdin(env []string, r *os.File, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.Stdin = r
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %v: %.300s", name, args, err, out)
	}
	return nil
}

// RunDir igual que Run pero con cmd.Dir fijado.
func RunDir(env []string, dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Alive comprueba si el daemon rootless responde en el socket de la sesión.
func Alive(env []string) bool {
	out, err := Run(env, "docker", "version", "--format", "{{.Server.Version}}")
	return err == nil && strings.TrimSpace(out) != ""
}

// StartDaemon arranca dockerd rootless (vía dockerd-rootless.sh) con todo el
// estado dentro del directorio de ejecución (tmpfs). Si ya está vivo, no hace nada.
func StartDaemon(runDir string) error {
	env := Env(runDir)
	if Alive(env) {
		return nil
	}
	logPath := filepath.Join(runDir, "dockerd-rootless.log")
	logF, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.Command("dockerd-rootless.sh",
		"--host=unix://"+run.Socket(runDir),
		"--data-root="+filepath.Join(runDir, "dockerd-data"),
		"--exec-root="+filepath.Join(runDir, "dockerd-exec"),
		"--pidfile="+filepath.Join(runDir, "dockerd.pid"),
	)
	cmd.Env = append(env, "DOCKERD_ROOTLESS_ROOTLESSKIT_STATE_DIR="+filepath.Join(runDir, "rk-state"))
	cmd.Stdout = logF
	cmd.Stderr = logF
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("arrancando dockerd-rootless.sh: %w (¿está en PATH o ~/.local/bin?)", err)
	}
	// esperar a que el socket responda (hasta 90 s)
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if Alive(env) {
			return nil
		}
		if cmd.ProcessState != nil && cmd.ProcessState.Exited() {
			return fmt.Errorf("dockerd rootless terminó prematuramente; ver %s", logPath)
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("el daemon rootless no respondió en 90 s; ver %s", logPath)
}

// KillOrphanDaemons mata (por pid, NUNCA pkill -f) cualquier proceso cuyo
// cmdline contenga "/tmp/flowsint-desk-" excepto el nuestro: son daemons
// rootless huérfanos de sesiones anteriores (kill -9) cuyo socket ya no
// es alcanzable por path (el dir pudo renombrarse a .stale).
func KillOrphanDaemons() []int {
	self := os.Getpid()
	var killed []int
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return killed
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var pid int
		if _, err := fmt.Sscanf(e.Name(), "%d", &pid); err != nil || pid == self {
			continue
		}
		cmdline, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if err != nil {
			continue
		}
		if strings.Contains(string(cmdline), "/tmp/flowsint-desk-") {
			_ = syscall.Kill(pid, syscall.SIGTERM)
			killed = append(killed, pid)
		}
	}
	if len(killed) > 0 {
		time.Sleep(3 * time.Second)
		for _, pid := range killed {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
	return killed
}

// StopDaemon para el daemon rootless de la sesión (SIGTERM y luego SIGKILL).
func StopDaemon(runDir string) {
	pidPath := filepath.Join(runDir, "dockerd.pid")
	b, err := os.ReadFile(pidPath)
	if err != nil {
		return
	}
	var pid int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &pid); err != nil || pid <= 0 {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

// CacheDir es la caché local de imágenes (~/ .cache/flowsint-desk/images).
func CacheDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(home, ".cache", "flowsint-desk", "images")
	if err := os.MkdirAll(d, 0o700); err != nil {
		return "", err
	}
	return d, nil
}

func cacheFile(cacheDir, image string) string {
	name := strings.NewReplacer("/", "_", ":", "_", ".", "_").Replace(image)
	return filepath.Join(cacheDir, name+".tar")
}

// EnsureImages asegura que las imágenes estén en el daemon rootless:
// primero mira el daemon, luego la caché en disco (docker load), y si no,
// hace pull y guarda el tar en la caché.
func EnsureImages(env []string, cacheDir string, logf func(string, ...any)) error {
	for _, img := range Images {
		if _, err := Run(env, "docker", "image", "inspect", img); err == nil {
			continue
		}
		tar := cacheFile(cacheDir, img)
		if _, err := os.Stat(tar); err == nil {
			logf("cargando %s desde la caché %s", img, tar)
			if out, err := Run(env, "docker", "load", "-i", tar); err != nil {
				return fmt.Errorf("docker load %s: %v: %s", img, err, out)
			}
			continue
		}
		logf("descargando %s (se guardará en caché)", img)
		if out, err := Run(env, "docker", "pull", img); err != nil {
			return fmt.Errorf("docker pull %s: %v: %s", img, err, out)
		}
		if out, err := Run(env, "docker", "save", "-o", tar, img); err != nil {
			return fmt.Errorf("docker save %s: %v: %s", img, err, out)
		}
	}
	return nil
}

// ImageDigests devuelve los RepoDigests de las imágenes (para el manifest).
func ImageDigests(env []string) (map[string]string, error) {
	out := map[string]string{}
	for _, img := range Images {
		o, err := Run(env, "docker", "image", "inspect", "--format", "{{join .RepoDigests \"\\n\"}}", img)
		if err != nil {
			return nil, fmt.Errorf("docker inspect %s: %w", img, err)
		}
		out[img] = strings.TrimSpace(o)
	}
	return out, nil
}

// Compose ejecuta `docker compose` con el proyecto escrito en stackDir y el
// env-file de secretos (tmpfs 0600). Devuelve salida combinada.
func Compose(env []string, runDir string, args ...string) (string, error) {
	stackDir := filepath.Join(runDir, "stack")
	full := append([]string{
		"compose",
		"--project-directory", stackDir,
		"-f", filepath.Join(stackDir, "compose.yml"),
		"--env-file", filepath.Join(runDir, "compose.env"),
	}, args...)
	return RunDir(env, stackDir, "docker", full...)
}
