// Package dock gestiona el Docker rootless efímero de la sesión:
// arranque/parada del daemon con data-root en tmpfs, caché de imágenes en
// disco (no contienen datos de usuario) y ejecución de `docker compose`.
// NUNCA usa el socket rootful del sistema: siempre DOCKER_HOST al socket
// rootless del directorio de ejecución.
package dock

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"flowsint-desk/internal/run"
)

// Images devuelve las imágenes del stack para una versión de Flowsint (la
// etiqueta de ghcr.io/reconurge/flowsint-{api,app}: "latest" o p. ej. "1.2.10").
// alpine:3 se usa solo para limpiar el data-root desde dentro del user-ns.
func Images(flowsintVersion string) []string {
	if flowsintVersion == "" {
		flowsintVersion = "latest"
	}
	imgs := []string{
		"alpine:3",
		"postgres:15-alpine",
		"redis:7-alpine",
		"neo4j:5",
		"ghcr.io/reconurge/flowsint-api:" + flowsintVersion,
		"ghcr.io/reconurge/flowsint-app:" + flowsintVersion,
	}
	if !IsPodman() {
		return imgs
	}
	// Podman: nombres completos, y sin alpine (no hace falta el contenedor
	// auxiliar para borrar: se usa `podman unshare`)
	out := make([]string, 0, len(imgs)-1)
	for _, img := range imgs[1:] {
		out = append(out, qualify(img))
	}
	return out
}

// WipeSubdirs borra subdirectorios de parent DENTRO del user-namespace del
// daemon rootless (un único contenedor), porque desde el host los ficheros del
// data-root pertenecen a uids mapeados (subuid) y rm -rf falla con
// "Permiso denegado". El daemon debe estar vivo. Se hace en UNA sola ejecución:
// borrar el data-root por partes corrompe el daemon vivo.
func WipeSubdirs(env []string, parent string, subs ...string) error {
	if IsPodman() {
		// Podman: se borra desde dentro del user namespace del usuario, sin
		// contenedor ni daemon (funciona también tras un kill -9).
		if !safeRunDir(parent) {
			return fmt.Errorf("REFUSO borrar fuera de un directorio de ejecución: %q", parent)
		}
		// El almacén overlay queda montado dentro del namespace de montaje
		// rootless: sin desmontarlo antes, rm falla con "resource busy".
		// De paso se anota el pid del proceso "pause" de ESTA sesión (el que
		// mantiene su user namespace; `podman unshare` lo crea si no existe):
		// hay que matarlo al final o se queda vivo para siempre.
		args := []string{"unshare", "sh", "-c",
			`cat "${XDG_RUNTIME_DIR:-/nonexistent}/libpod/tmp/pause.pid" 2>/dev/null; echo; umount -l "$0/podman-data/root/overlay" 2>/dev/null; rm -rf "$@" >&2`, parent}
		var dirs []string
		for _, s := range append(subs, "podman-data") {
			dirs = append(dirs, filepath.Join(parent, s))
		}
		cmd := exec.Command("podman", append(args, dirs...)...)
		cmd.Env = env
		var so, se bytes.Buffer
		cmd.Stdout, cmd.Stderr = &so, &se
		err := cmd.Run()
		killPauseIfUnused(strings.TrimSpace(so.String()))
		out := se.String()
		if err == nil {
			return nil
		}
		// lo que cuenta es que no quede nada: si desde fuera ya se puede
		// borrar el resto, el aviso de rm no es un fallo
		for _, d := range dirs {
			if os.RemoveAll(d) != nil {
				return fmt.Errorf("podman unshare rm en %s: %v: %s", parent, err, strings.TrimSpace(out))
			}
		}
		return nil
	}
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

// killPauseIfUnused mata el proceso "pause" de Podman (el que mantiene el
// user namespace rootless) para que al cerrar no quede ningún proceso vivo.
// Ese proceso es UNO por usuario, compartido con el Podman personal: solo se
// mata si no queda ningún otro contenedor Podman del usuario en marcha (ningún
// conmon). Si quedan, se deja: es suyo. Podman lo vuelve a crear solo la
// próxima vez que haga falta.
func killPauseIfUnused(pidStr string) {
	var pid int
	if _, err := fmt.Sscanf(pidStr, "%d", &pid); err != nil || pid <= 1 {
		return
	}
	comm, err := os.ReadFile(fmt.Sprintf("/proc/%d/comm", pid))
	if err != nil {
		return
	}
	// nunca a ciegas: tiene que ser de verdad un pause
	if c := strings.TrimSpace(string(comm)); c != "catatonit" && c != "podman" {
		return
	}
	if otherContainersRunning() {
		return
	}
	_ = syscall.Kill(pid, syscall.SIGTERM)
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	if x := os.Getenv("XDG_RUNTIME_DIR"); x != "" {
		_ = os.Remove(filepath.Join(x, "libpod", "tmp", "pause.pid"))
	}
}

// otherContainersRunning: queda algún conmon (un vigilante por contenedor
// Podman) de este usuario. En caso de duda responde true (no se mata nada).
func otherContainersRunning() bool {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return true
	}
	uid := uint32(os.Getuid())
	for _, e := range entries {
		var pid int
		if _, err := fmt.Sscanf(e.Name(), "%d", &pid); err != nil {
			continue
		}
		comm, err := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
		if err != nil || strings.TrimSpace(string(comm)) != "conmon" {
			continue
		}
		fi, err := os.Stat(filepath.Join("/proc", e.Name()))
		if err != nil {
			continue
		}
		if st, ok := fi.Sys().(*syscall.Stat_t); !ok || st.Uid == uid {
			return true
		}
	}
	return false
}

// Env devuelve el entorno para invocar docker apuntando SOLO al rootless.
func Env(runDir string) []string {
	if IsPodman() {
		return podmanEnv(runDir)
	}
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
	cmd := exec.Command(bin(name), args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Output ejecuta un comando y devuelve SOLO stdout; stderr va al error. Para
// salidas que se parsean (recuentos): un aviso por stderr no debe colarse
// entre los datos.
func Output(env []string, name string, args ...string) (string, error) {
	cmd := exec.Command(bin(name), args...)
	cmd.Env = env
	var so, se bytes.Buffer
	cmd.Stdout = &so
	cmd.Stderr = &se
	if err := cmd.Run(); err != nil {
		return so.String(), fmt.Errorf("%v: %.300s", err, strings.TrimSpace(se.String()))
	}
	return so.String(), nil
}

// RunToFile ejecuta un comando volcando SOLO stdout a path (0600), en
// streaming (sin cargar el volcado en memoria). stderr va al error. Si el
// comando falla, el fichero parcial se borra.
func RunToFile(env []string, path string, name string, args ...string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.Command(bin(name), args...)
	cmd.Env = env
	var se bytes.Buffer
	cmd.Stdout = f
	cmd.Stderr = &se
	runErr := cmd.Run()
	closeErr := f.Close()
	if runErr != nil {
		_ = os.Remove(path)
		return fmt.Errorf("%v: %.300s", runErr, strings.TrimSpace(se.String()))
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return closeErr
	}
	return nil
}

// Logs devuelve las últimas `tail` líneas de los logs de un contenedor
// (json-file dentro del data-root tmpfs: nunca tocan el disco local).
func Logs(env []string, name string, tail int) (string, error) {
	return Run(env, "docker", "logs", "--tail", fmt.Sprint(tail), name)
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
	cmd := exec.Command(bin(name), args...)
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
	cmd := exec.Command(bin(name), args...)
	cmd.Env = env
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Alive comprueba si el daemon rootless responde en el socket de la sesión.
func Alive(env []string) bool {
	if IsPodman() {
		// sin daemon: "vivo" = el almacén de la sesión es utilizable
		out, err := Run(env, "podman", "info", "--format", "{{.Store.GraphRoot}}")
		return err == nil && strings.TrimSpace(out) != ""
	}
	out, err := Run(env, "docker", "version", "--format", "{{.Server.Version}}")
	return err == nil && strings.TrimSpace(out) != ""
}

// StartDaemon arranca dockerd rootless (vía dockerd-rootless.sh) con todo el
// estado dentro del directorio de ejecución (tmpfs). Si ya está vivo, no hace nada.
func StartDaemon(runDir string) error {
	if IsPodman() {
		return startPodmanService(runDir)
	}
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
		_ = logF.Close()
		return fmt.Errorf("arrancando dockerd-rootless.sh: %w (¿está en PATH o ~/.local/bin?)", err)
	}
	_ = logF.Close() // el hijo conserva su copia del descriptor
	// Wait en segundo plano: sin él ProcessState nunca se rellena y una muerte
	// inmediata del daemon no se detectaba hasta agotar los 90 s.
	muerto := make(chan struct{})
	go func() { _ = cmd.Wait(); close(muerto) }()
	// El log vive en el directorio de ejecución, que se borra si el arranque
	// falla: el error se lleva las últimas líneas (no contienen secretos).
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if Alive(env) {
			return nil
		}
		select {
		case <-muerto:
			return fmt.Errorf("dockerd rootless terminó prematuramente. Últimas líneas de su log:\n%s", tailFile(logPath, 25))
		case <-time.After(1 * time.Second):
		}
	}
	return fmt.Errorf("el daemon rootless no respondió en 90 s. Últimas líneas de su log:\n%s", tailFile(logPath, 25))
}

// tailFile devuelve las últimas n líneas de un fichero de texto pequeño.
func tailFile(path string, n int) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "(no se pudo leer " + path + ")"
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// KillOrphanDaemons mata (por pid, NUNCA pkill -f) cualquier proceso cuyo
// cmdline contenga match, excepto el nuestro: son daemons rootless huérfanos
// (dockerd, rootlesskit, slirp4netns, containerd...) de una sesión que murió
// o que se está desmontando. match debe ser una ruta de directorio de
// ejecución: "/tmp/flowsint-desk-" para cualquier sesión, o la de una
// concreta. Devuelve solo los pids a los que se pudo enviar la señal.
func KillOrphanDaemons(match string) []int {
	self := os.Getpid()
	var killed []int
	if match == "" {
		return killed
	}
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
		if strings.Contains(string(cmdline), match) {
			if syscall.Kill(pid, syscall.SIGTERM) == nil {
				killed = append(killed, pid)
			}
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

func digestFile(cacheDir, image string) string { return cacheFile(cacheDir, image) + ".digest" }

// repoDigest devuelve el primer RepoDigest ("repo@sha256:…") de una imagen
// presente en el daemon, o "".
func repoDigest(env []string, img string) string {
	out, err := Output(env, "docker", "image", "inspect", "--format", "{{range .RepoDigests}}{{println .}}{{end}}", img)
	if err != nil {
		return ""
	}
	if f := strings.Fields(out); len(f) > 0 {
		return f[0]
	}
	return ""
}

// saveToCache guarda la imagen en la caché de disco junto con su digest.
// `docker load` no conserva los RepoDigests, así que el digest con el que se
// descargó se anota en un fichero aparte: es lo que permite saber (y fijar)
// qué imagen exacta se está ejecutando.
func saveToCache(env []string, cacheDir, img string) error {
	tar := cacheFile(cacheDir, img)
	if out, err := Run(env, "docker", "save", "-o", tar, img); err != nil {
		return fmt.Errorf("docker save %s: %v: %s", img, err, out)
	}
	if sum, err := fileSHA256(tar); err == nil {
		_ = os.WriteFile(sumFile(cacheDir, img), []byte(sum+"\n"), 0o600)
	}
	if d := repoDigest(env, img); d != "" {
		return os.WriteFile(digestFile(cacheDir, img), []byte(d+"\n"), 0o600)
	}
	return nil
}

func sumFile(cacheDir, image string) string { return cacheFile(cacheDir, image) + ".sha256" }

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// verifyCached comprueba que el tar de la caché es el mismo que se guardó al
// descargarlo. Una caché de una versión anterior (sin suma) se da por buena y
// se le anota la suma ahora.
func verifyCached(cacheDir, img string) error {
	tar := cacheFile(cacheDir, img)
	got, err := fileSHA256(tar)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(sumFile(cacheDir, img))
	if err != nil {
		return os.WriteFile(sumFile(cacheDir, img), []byte(got+"\n"), 0o600)
	}
	if want := strings.TrimSpace(string(b)); want != got {
		return fmt.Errorf("la imagen en caché de %s NO coincide con la que se descargó (sha256 %s…, esperado %s…): no se carga. Vuelve a descargarla con `flowsint-desk images update`", img, got[:12], want[:min(12, len(want))])
	}
	return nil
}

// EnsureImages asegura que las imágenes estén en el daemon rootless:
// primero mira el daemon, luego la caché en disco (docker load), y si no,
// hace pull y guarda el tar (y su digest) en la caché.
func EnsureImages(env []string, cacheDir string, images []string, logf func(string, ...any)) error {
	for _, img := range images {
		if _, err := Run(env, "docker", "image", "inspect", img); err == nil {
			continue
		}
		tar := cacheFile(cacheDir, img)
		if _, err := os.Stat(tar); err == nil {
			logf("cargando %s desde la caché", img)
			if err := verifyCached(cacheDir, img); err != nil {
				return err
			}
			if out, err := Run(env, "docker", "load", "-i", tar); err != nil {
				return fmt.Errorf("docker load %s: %v: %s", img, err, out)
			}
			continue
		}
		logf("descargando %s (se guardará en caché)", img)
		if out, err := Run(env, "docker", "pull", img); err != nil {
			return fmt.Errorf("docker pull %s: %v: %s", img, err, out)
		}
		if err := saveToCache(env, cacheDir, img); err != nil {
			return err
		}
	}
	return nil
}

// PullExact descarga la imagen por su digest (ref = "repo@sha256:…"), la
// etiqueta como img y la deja en la caché. Sirve para que un equipo ejecute
// exactamente la misma imagen que otro.
func PullExact(env []string, cacheDir, img, ref string) error {
	if !strings.Contains(ref, "@sha256:") {
		return fmt.Errorf("referencia sin digest: %q", ref)
	}
	if out, err := Run(env, "docker", "pull", ref); err != nil {
		return fmt.Errorf("docker pull %s: %v: %s", ref, err, out)
	}
	if out, err := Run(env, "docker", "tag", ref, img); err != nil {
		return fmt.Errorf("docker tag %s %s: %v: %s", ref, img, err, out)
	}
	return saveToCache(env, cacheDir, img)
}

// InCache dice si la imagen ya está guardada en la caché de disco.
func InCache(cacheDir, img string) bool {
	_, err := os.Stat(cacheFile(cacheDir, img))
	return err == nil
}

// DropCache borra de la caché el tar y el digest de una imagen (para forzar
// una descarga nueva).
func DropCache(cacheDir, img string) {
	_ = os.Remove(cacheFile(cacheDir, img))
	_ = os.Remove(digestFile(cacheDir, img))
	_ = os.Remove(sumFile(cacheDir, img))
}

// CachedDigests devuelve el digest anotado en la caché para cada imagen
// ("" si no se conoce: caché creada por una versión anterior). No necesita
// daemon.
func CachedDigests(cacheDir string, images []string) map[string]string {
	out := map[string]string{}
	for _, img := range images {
		b, _ := os.ReadFile(digestFile(cacheDir, img))
		out[img] = strings.TrimSpace(string(b))
	}
	return out
}

// ImageDigests devuelve el digest de cada imagen (para el manifest): el que
// conoce el daemon o, si la imagen vino de la caché, el anotado al descargarla.
func ImageDigests(env []string, cacheDir string, images []string) map[string]string {
	out := CachedDigests(cacheDir, images)
	for _, img := range images {
		if d := repoDigest(env, img); d != "" {
			out[img] = d
		}
	}
	return out
}

// Drift compara los digests con los que se creó un snapshot (want) con los
// locales (have) y devuelve las imágenes que difieren. Solo cuenta cuando
// ambos lados conocen el digest.
func Drift(want, have map[string]string) []string {
	// la misma imagen se llama distinto según el motor (con o sin docker.io/)
	h := map[string]string{}
	for img, d := range have {
		h[normalize(img)] = d
	}
	var out []string
	for img, w := range want {
		if d := h[normalize(img)]; w != "" && d != "" && digestOf(w) != digestOf(d) {
			out = append(out, img)
		}
	}
	sort.Strings(out)
	return out
}

// digestOf se queda con "sha256:…" de "repo@sha256:…".
func digestOf(ref string) string {
	if i := strings.LastIndex(ref, "@"); i >= 0 {
		return ref[i+1:]
	}
	return ref
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
