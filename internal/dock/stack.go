package dock

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"flowsint-desk/internal/run"
)

// Este fichero es la única parte que sabe CÓMO se levanta el stack en cada
// motor. Con Docker se delega en `docker compose` (compose embebido, validado
// en las fases del encargo). Con Podman la CLI lanza los seis contenedores
// ella misma: así no depende de la API de Docker que Podman emula, ni de sus
// healthchecks (que necesitan temporizadores de systemd de usuario).

const (
	netName = "flowsint-desk-net"
	prefix  = "flowsint-desk-"
)

// Services en orden de arranque.
var Services = []string{"postgres", "redis", "neo4j", "api", "celery", "app"}

// startPodmanService arranca `podman system service`: el socket con API de
// Docker que necesitan los enrichers de Flowsint (lo usan a través del proxy
// filtrado). Es un proceso prescindible: los contenedores no dependen de él.
func startPodmanService(runDir string) error {
	env := Env(runDir)
	pidPath := filepath.Join(runDir, "dockerd.pid") // mismo nombre: StopDaemon vale para los dos motores
	if b, err := os.ReadFile(pidPath); err == nil {
		var pid int
		if _, err := fmt.Sscanf(strings.TrimSpace(string(b)), "%d", &pid); err == nil && pid > 0 && processAlive(pid) {
			return nil
		}
	}
	if !Alive(env) {
		out, _ := Run(env, "podman", "info")
		return fmt.Errorf("podman no puede usar el almacén de la sesión en %s:\n%s", podmanDir(runDir), lastLines(out, 12))
	}
	sock := run.Socket(runDir)
	_ = os.Remove(sock)
	logPath := filepath.Join(runDir, "podman-service.log")
	logF, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	cmd := exec.Command("podman", "system", "service", "--time=0", "unix://"+sock)
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = logF, logF
	if err := cmd.Start(); err != nil {
		_ = logF.Close()
		return fmt.Errorf("arrancando podman system service: %w", err)
	}
	_ = logF.Close()
	if err := os.WriteFile(pidPath, []byte(fmt.Sprint(cmd.Process.Pid)), 0o600); err != nil {
		return err
	}
	muerto := make(chan struct{})
	go func() { _ = cmd.Wait(); close(muerto) }()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sock); err == nil {
			return nil
		}
		select {
		case <-muerto:
			return fmt.Errorf("podman system service terminó prematuramente. Últimas líneas de su log:\n%s", tailFile(logPath, 25))
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("el socket de podman no apareció en 60 s. Últimas líneas de su log:\n%s", tailFile(logPath, 25))
}

func processAlive(pid int) bool {
	_, err := os.Stat(fmt.Sprintf("/proc/%d", pid))
	return err == nil
}

func lastLines(s string, n int) string {
	l := strings.Split(strings.TrimSpace(s), "\n")
	if len(l) > n {
		l = l[len(l)-n:]
	}
	return strings.Join(l, "\n")
}

// StackUp levanta los servicios indicados y espera a que estén listos.
// Devuelve la salida del motor (para mostrarla, saneada, si falla).
func StackUp(env []string, runDir string, services ...string) (string, error) {
	if !IsPodman() {
		return Compose(env, runDir, append([]string{"up", "-d", "--wait"}, services...)...)
	}
	vals, err := readEnvFile(filepath.Join(runDir, "compose.env"))
	if err != nil {
		return "", err
	}
	if out, err := Run(env, "podman", "network", "exists", netName); err != nil {
		if out, err = Run(env, "podman", "network", "create", netName); err != nil {
			return out, fmt.Errorf("creando la red %s: %v", netName, err)
		}
	}
	for _, name := range services {
		spec, ok := podmanSpec(name, runDir, vals)
		if !ok {
			return "", fmt.Errorf("servicio desconocido %q", name)
		}
		envFile := filepath.Join(runDir, "stack", name+".env")
		if err := os.WriteFile(envFile, []byte(strings.Join(spec.env, "\n")+"\n"), 0o600); err != nil {
			return "", err
		}
		args := []string{"run", "-d", "--name", prefix + name,
			"--network", netName, "--network-alias", name,
			"--security-opt", "no-new-privileges",
			// sin healthchecks del motor: los haría systemd de usuario con
			// temporizadores; la CLI comprueba ella misma que el servicio responde
			"--no-healthcheck",
			"--log-driver", "k8s-file",
			// los secretos van en un fichero 0600 de la tmpfs, nunca en argv
			"--env-file", envFile}
		if spec.binds && selinuxEnabled() {
			// montajes de la tmpfs sin etiqueta: con SELinux hay que relajar
			// el confinamiento de ESTE contenedor o no podrá leerlos
			args = append(args, "--security-opt", "label=disable")
		}
		args = append(args, spec.args...)
		args = append(args, spec.image)
		args = append(args, spec.cmd...)
		if out, err := Run(env, "podman", args...); err != nil {
			return out, fmt.Errorf("arrancando %s: %v", name, err)
		}
		if err := waitReady(env, name, spec); err != nil {
			return "", err
		}
	}
	return "", nil
}

// waitReady espera a que el servicio responda a su comprobación (o, si no
// tiene, a que el contenedor siga en marcha). Falla en cuanto el contenedor
// muere, sin agotar el plazo.
func waitReady(env []string, name string, spec svcSpec) error {
	cont := prefix + name
	deadline := time.Now().Add(spec.timeout)
	var last string
	for {
		st, _ := Output(env, "podman", "inspect", "--format", "{{.State.Status}}", cont)
		st = strings.TrimSpace(st)
		if st == "exited" || st == "stopped" || st == "dead" {
			return fmt.Errorf("el contenedor %s ha terminado solo (estado %s)", cont, st)
		}
		if st == "running" {
			if len(spec.probe) == 0 {
				return nil
			}
			out, err := Run(env, "podman", append([]string{"exec", cont}, spec.probe...)...)
			if err == nil {
				return nil
			}
			last = lastLines(out, 2)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s no ha respondido en %s (última comprobación: %s)", cont, spec.timeout, last)
		}
		time.Sleep(3 * time.Second)
	}
}

// StackDown para y borra los contenedores de la sesión (los del stack y los
// que hayan lanzado los enrichers) y la red. Los volúmenes se van con el wipe.
func StackDown(env []string, runDir string) (string, error) {
	if !IsPodman() {
		return Compose(env, runDir, "down", "--remove-orphans")
	}
	// Los datos ya están en el snapshot: no hace falta una parada elegante de
	// las BD (Neo4j tarda más de lo que merece esperar). Se reintenta una vez
	// y solo es un fallo si al final queda algún contenedor.
	out, err := Run(env, "podman", "rm", "-fa", "-t", "2")
	if err != nil {
		out, err = Run(env, "podman", "rm", "-fa", "-t", "0")
	}
	_, _ = Run(env, "podman", "network", "rm", "-f", netName)
	if quedan, e := Output(env, "podman", "ps", "-aq"); e == nil && strings.TrimSpace(quedan) == "" {
		return "", nil
	}
	return out, err
}

// StackReset deja el motor sin restos del proyecto antes de un arranque:
// contenedores, volúmenes (p. ej. el auth.ini de Neo4j con otra contraseña
// rompe el arranque) y red.
func StackReset(env []string, runDir string) {
	if !IsPodman() {
		_, _ = Compose(env, runDir, "down", "-v", "--remove-orphans")
		for _, v := range []string{"pg_data", "neo4j_data", "neo4j_logs", "neo4j_plugins"} {
			_, _ = Run(env, "docker", "volume", "rm", "flowsint-desk_"+v)
		}
		_, _ = Run(env, "docker", "network", "rm", netName)
		return
	}
	_, _ = Run(env, "podman", "rm", "-fa", "-t", "0")
	_, _ = Run(env, "podman", "volume", "rm", "-fa")
	_, _ = Run(env, "podman", "network", "rm", "-f", netName)
}

type svcSpec struct {
	image   string
	env     []string // KEY=VALOR, a un --env-file
	args    []string // opciones extra de `podman run`
	cmd     []string
	probe   []string // comando dentro del contenedor que debe salir con 0
	timeout time.Duration
	binds   bool // monta rutas del host (socket, import de Neo4j)
}

// podmanSpec traduce cada servicio de assets/compose.yml a un `podman run`.
// DEBE mantenerse igual que el compose: mismas imágenes, variables, volúmenes,
// alias de red y único puerto publicado en loopback.
func podmanSpec(name, runDir string, v map[string]string) (svcSpec, bool) {
	ver := v["FLOWSINT_VERSION"]
	if ver == "" {
		ver = "latest"
	}
	apiImage := "ghcr.io/reconurge/flowsint-api:" + ver
	dbURL := fmt.Sprintf("postgresql://%s:%s@postgres:5432/%s", v["POSTGRES_USER"], v["POSTGRES_PASSWORD"], v["POSTGRES_DB"])
	apiEnv := []string{
		"DATABASE_URL=" + dbURL,
		"NEO4J_URI_BOLT=bolt://neo4j:7687",
		"NEO4J_USERNAME=" + v["NEO4J_USERNAME"],
		"NEO4J_PASSWORD=" + v["NEO4J_PASSWORD"],
		"AUTH_SECRET=" + v["AUTH_SECRET"],
		"MASTER_VAULT_KEY_V1=" + v["MASTER_VAULT_KEY_V1"],
		"REDIS_URL=redis://redis:6379/0",
		"DOCKER_HOST=unix:///var/run/docker.sock",
	}
	sockMount := []string{"-v", v["DESK_SOCK"] + ":/var/run/docker.sock"}
	const neoAuth = `-u "${NEO4J_AUTH%%/*}" -p "${NEO4J_AUTH#*/}"`
	switch name {
	case "postgres":
		return svcSpec{
			image: qualify("postgres:15-alpine"),
			env: []string{"POSTGRES_USER=" + v["POSTGRES_USER"], "POSTGRES_PASSWORD=" + v["POSTGRES_PASSWORD"],
				"POSTGRES_DB=" + v["POSTGRES_DB"]},
			args:    []string{"-v", "flowsint-desk_pg_data:/var/lib/postgresql/data"},
			probe:   []string{"sh", "-c", `pg_isready -U "$POSTGRES_USER"`},
			timeout: 2 * time.Minute,
		}, true
	case "redis":
		return svcSpec{
			image: qualify("redis:7-alpine"),
			// sin persistencia: los resultados de Celery pueden llevar datos OSINT
			cmd:     []string{"redis-server", "--save", "", "--appendonly", "no"},
			probe:   []string{"redis-cli", "ping"},
			timeout: 1 * time.Minute,
		}, true
	case "neo4j":
		return svcSpec{
			image: qualify("neo4j:5"),
			env: []string{"NEO4J_AUTH=" + v["NEO4J_USERNAME"] + "/" + v["NEO4J_PASSWORD"],
				`NEO4J_PLUGINS=["apoc"]`,
				"NEO4J_apoc_export_file_enabled=true",
				"NEO4J_apoc_import_file_enabled=true",
				"NEO4J_apoc_import_file_use__neo4j__config=true"},
			args: []string{"-v", "flowsint-desk_neo4j_data:/data", "-v", "flowsint-desk_neo4j_logs:/logs",
				"-v", filepath.Join(runDir, "neo4j-import") + ":/var/lib/neo4j/import",
				"-v", "flowsint-desk_neo4j_plugins:/plugins"},
			probe:   []string{"sh", "-c", "cypher-shell " + neoAuth + " 'RETURN 1'"},
			timeout: 5 * time.Minute,
			binds:   true,
		}, true
	case "api":
		return svcSpec{
			image: apiImage, env: apiEnv, args: sockMount,
			probe:   []string{"sh", "-c", "curl -fsS http://localhost:5001/health >/dev/null"},
			timeout: 4 * time.Minute,
			binds:   true,
		}, true
	case "celery":
		return svcSpec{
			image: apiImage, env: append(apiEnv, "SKIP_MIGRATIONS=true"), args: sockMount,
			cmd:     []string{"celery", "-A", "flowsint_core.core.celery", "worker", "--loglevel=info", "--pool=threads", "--concurrency=10"},
			timeout: 1 * time.Minute,
			binds:   true,
		}, true
	case "app":
		return svcSpec{
			image: "ghcr.io/reconurge/flowsint-app:" + ver,
			// único puerto publicado, solo en loopback
			args:    []string{"-p", "127.0.0.1:" + v["DESK_PORT"] + ":8080"},
			timeout: 1 * time.Minute,
		}, true
	}
	return svcSpec{}, false
}

func selinuxEnabled() bool {
	_, err := os.Stat("/sys/fs/selinux/enforce")
	return err == nil
}

// readEnvFile lee un fichero KEY=VALOR (el compose.env del run dir).
func readEnvFile(path string) (map[string]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		if k, val, ok := strings.Cut(strings.TrimSpace(line), "="); ok && !strings.HasPrefix(k, "#") {
			m[k] = val
		}
	}
	return m, nil
}

// ContainerNames devuelve los nombres de los contenedores del stack.
func ContainerNames() []string {
	out := make([]string, 0, len(Services))
	for _, s := range Services {
		out = append(out, prefix+s)
	}
	sort.Strings(out)
	return out
}
