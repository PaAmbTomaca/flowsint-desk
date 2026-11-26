package dock

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"flowsint-desk/internal/run"
)

// Motores de contenedores soportados.
const (
	EngineDocker = "docker" // Docker rootless: dockerd por sesión + docker compose
	EnginePodman = "podman" // Podman rootless: sin daemon; la CLI lanza los contenedores
)

var engine = EngineDocker

// ValidEngine dice si e es un motor conocido.
func ValidEngine(e string) bool { return e == EngineDocker || e == EnginePodman }

// SetEngine fija el motor para todo el proceso (lo hace la CLI al arrancar,
// según config.toml).
func SetEngine(e string) {
	if ValidEngine(e) {
		engine = e
	}
}

// Engine devuelve el motor activo.
func Engine() string { return engine }

// IsPodman: el motor activo es Podman.
func IsPodman() bool { return engine == EnginePodman }

// bin traduce el nombre del cliente: todo el código pide "docker" y, con el
// motor Podman, se ejecuta "podman" (misma sintaxis para run/exec/cp/logs/
// pull/save/load/inspect, que es lo único que se usa).
func bin(name string) string {
	if name == "docker" && engine == EnginePodman {
		return "podman"
	}
	return name
}

// podmanDir es el único subdirectorio (tmpfs) donde Podman guarda TODO su
// estado de la sesión: imágenes, contenedores, volúmenes, redes y temporales.
func podmanDir(runDir string) string { return filepath.Join(runDir, "podman-data") }

// ensurePodmanConf escribe la configuración que aísla a Podman dentro del
// directorio de ejecución y devuelve las rutas de los dos ficheros:
//   - storage.conf: graphroot y runroot en la tmpfs de la sesión (no en
//     ~/.local/share/containers: ni se mezcla con el Podman del usuario ni
//     toca el disco);
//   - containers.conf (se aplica ENCIMA de la del sistema, vía
//     CONTAINERS_CONF_OVERRIDE): volúmenes, redes y temporales también ahí, y
//     logs y eventos a FICHERO. El valor por defecto en sistemas con systemd
//     es journald, que escribiría la salida de los contenedores en el diario
//     del sistema, en disco y en claro.
func ensurePodmanConf(runDir string) (storage, containers string) {
	d := podmanDir(runDir)
	_ = os.MkdirAll(d, 0o700)
	storage = filepath.Join(d, "storage.conf")
	containers = filepath.Join(d, "containers.conf")
	sc := fmt.Sprintf("[storage]\ndriver = \"overlay\"\ngraphroot = %q\nrunroot = %q\n",
		filepath.Join(d, "root"), filepath.Join(d, "run"))
	cc := fmt.Sprintf("[containers]\nlog_driver = \"k8s-file\"\n\n[engine]\ntmp_dir = %q\nvolume_path = %q\nevents_logger = \"file\"\n\n[network]\nnetwork_config_dir = %q\n",
		filepath.Join(d, "tmp"), filepath.Join(d, "volumes"), filepath.Join(d, "networks"))
	writeIfDifferent(storage, sc)
	writeIfDifferent(containers, cc)
	return storage, containers
}

func writeIfDifferent(path, content string) {
	if b, err := os.ReadFile(path); err == nil && string(b) == content {
		return
	}
	_ = os.WriteFile(path, []byte(content), 0o600)
}

// podmanEnv: entorno para invocar podman apuntando SOLO al estado de la sesión.
func podmanEnv(runDir string) []string {
	home, _ := os.UserHomeDir()
	storage, containers := ensurePodmanConf(runDir)
	env := []string{
		"CONTAINERS_STORAGE_CONF=" + storage,
		"CONTAINERS_CONF_OVERRIDE=" + containers,
		// para lo que aún habla la API de Docker (el SDK de los enrichers pasa
		// por el proxy; esto es solo por coherencia si alguien usa `docker`)
		"DOCKER_HOST=unix://" + run.Socket(runDir),
		"PATH=" + filepath.Join(home, ".local", "bin") + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + home,
		"USER=" + os.Getenv("USER"),
		"LOGNAME=" + os.Getenv("LOGNAME"),
		"SHELL=" + os.Getenv("SHELL"),
	}
	// XDG_RUNTIME_DIR tiene que ser el REAL del usuario: netavark arranca
	// aardvark-dns (el DNS entre contenedores) a través del systemd de usuario
	// y lo localiza ahí. Consecuencia: el proceso "pause" de Podman es uno por
	// usuario, compartido con su Podman personal (ver killPauseIfUnused).
	for _, k := range []string{"XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// qualify devuelve el nombre COMPLETO de una imagen. Docker asume docker.io;
// Podman, sin terminal, puede negarse a resolver un nombre corto
// ("short-name resolution enforced"), así que con Podman se usa siempre el
// nombre con registro.
func qualify(img string) string {
	first, _, hasSlash := strings.Cut(img, "/")
	switch {
	case !hasSlash:
		return "docker.io/library/" + img
	case strings.ContainsAny(first, ".:") || first == "localhost":
		return img
	}
	return "docker.io/" + img
}

// normalize quita el registro por defecto para poder comparar la misma imagen
// vista desde los dos motores (postgres:15 == docker.io/library/postgres:15).
func normalize(img string) string {
	img = strings.TrimPrefix(img, "docker.io/")
	return strings.TrimPrefix(img, "library/")
}

// safeRunDir: solo se borra dentro de un directorio de ejecución de la CLI.
func safeRunDir(p string) bool {
	return strings.HasPrefix(filepath.Clean(p), "/tmp/flowsint-desk-") && !strings.Contains(p, "..")
}
