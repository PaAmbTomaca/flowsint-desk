package dock

import (
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	"flowsint-desk/assets"
)

// composeService es lo que este test necesita saber de cada servicio del
// compose embebido. El fichero tiene un formato fijo y conocido: se lee por
// líneas e indentación, sin dependencia de un parser YAML.
type composeService struct {
	image   string
	envKeys []string
	targets []string // rutas DENTRO del contenedor de sus volúmenes
	caps    []string
	ports   []string
	command string
	noCore  bool
	dropAll bool
	noNewP  bool
}

func parseCompose(t *testing.T) map[string]*composeService {
	t.Helper()
	out := map[string]*composeService{}
	var cur *composeService
	section, inServices := "", false
	svcRe := regexp.MustCompile(`^  ([a-z0-9_-]+):\s*$`)
	for _, line := range strings.Split(string(assets.ComposeYML), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		switch {
		case line == "services:":
			inServices = true
			continue
		case inServices && line != "" && !strings.HasPrefix(line, " "):
			inServices = false
		}
		if !inServices {
			continue
		}
		if m := svcRe.FindStringSubmatch(line); m != nil {
			cur = &composeService{}
			out[m[1]] = cur
			section = ""
			continue
		}
		if cur == nil {
			continue
		}
		tr := strings.TrimSpace(line)
		if strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "     ") { // clave del servicio
			k, v, _ := strings.Cut(tr, ":")
			v = strings.TrimSpace(v)
			section = k
			switch k {
			case "image":
				cur.image = v
			case "command":
				cur.command = v
			case "ulimits":
				cur.noCore = strings.Contains(v, "core: 0")
			case "cap_drop":
				cur.dropAll = strings.Contains(v, "ALL")
			case "cap_add":
				for _, c := range strings.Split(strings.Trim(v, "[]"), ",") {
					cur.caps = append(cur.caps, strings.TrimSpace(c))
				}
			case "security_opt":
				cur.noNewP = strings.Contains(v, "no-new-privileges")
			}
			continue
		}
		item := strings.Trim(strings.TrimPrefix(tr, "- "), `"`)
		switch section {
		case "environment":
			k := item
			if i := strings.IndexAny(item, "=:"); i > 0 {
				k = item[:i]
			}
			if k != "" {
				cur.envKeys = append(cur.envKeys, strings.TrimSpace(k))
			}
		case "volumes":
			if p := strings.Split(item, ":"); len(p) >= 2 {
				cur.targets = append(cur.targets, p[1])
			}
		case "ports":
			if item != "" {
				cur.ports = append(cur.ports, item)
			}
		}
	}
	return out
}

func sorted(s []string) []string {
	o := append([]string{}, s...)
	sort.Strings(o)
	return o
}

// El compose (motor Docker) y podmanSpec (motor Podman) describen el MISMO
// stack. Se mantienen a mano: este test falla si uno cambia y el otro no.
func TestPodmanSpecIgualQueElCompose(t *testing.T) {
	const ver, port, sock, runDir = "9.9.9", "5273", "/run/enrichers.sock", "/run/x"
	vals := map[string]string{
		"FLOWSINT_VERSION": ver, "DESK_PORT": port, "DESK_SOCK": sock, "DESK_RUN": runDir,
		"POSTGRES_USER": "u", "POSTGRES_PASSWORD": "p", "POSTGRES_DB": "d",
		"NEO4J_USERNAME": "n", "NEO4J_PASSWORD": "np", "AUTH_SECRET": "a", "MASTER_VAULT_KEY_V1": "k",
	}
	compose := parseCompose(t)
	if len(compose) != len(Services) {
		t.Fatalf("el compose tiene %d servicios y la CLI conoce %d (%v)", len(compose), len(Services), Services)
	}
	// que el lector del compose lee de verdad (un parser que no ve nada haría
	// pasar el test por comparar vacío con vacío... no: por eso se exige contenido)
	if n := len(compose["api"].envKeys); n < 8 {
		t.Fatalf("el lector del compose solo ve %d variables en api: %v", n, compose["api"].envKeys)
	}
	if len(compose["neo4j"].targets) != 4 || len(compose["app"].ports) != 1 || len(compose["postgres"].caps) != 5 || compose["celery"].command == "" {
		t.Fatalf("el lector del compose no está leyendo volúmenes/puertos/capacidades/comando: %+v %+v %+v %q",
			compose["neo4j"].targets, compose["app"].ports, compose["postgres"].caps, compose["celery"].command)
	}
	subst := strings.NewReplacer("${FLOWSINT_VERSION}", ver, "${DESK_PORT}", port, "${DESK_SOCK}", sock, "${DESK_RUN}", runDir)
	for _, name := range Services {
		c, ok := compose[name]
		if !ok {
			t.Errorf("%s: no está en el compose", name)
			continue
		}
		spec, ok := podmanSpec(name, runDir, vals)
		if !ok {
			t.Errorf("%s: no está en podmanSpec", name)
			continue
		}
		if got, want := normalize(spec.image), normalize(subst.Replace(c.image)); got != want {
			t.Errorf("%s: imagen podman %q != compose %q", name, got, want)
		}
		var keys []string
		for _, e := range spec.env {
			k, _, _ := strings.Cut(e, "=")
			keys = append(keys, k)
		}
		if !reflect.DeepEqual(sorted(keys), sorted(c.envKeys)) {
			t.Errorf("%s: variables de entorno distintas\n  podman:  %v\n  compose: %v", name, sorted(keys), sorted(c.envKeys))
		}
		var targets, ports []string
		for i := 0; i+1 < len(spec.args); i++ {
			switch spec.args[i] {
			case "-v":
				if p := strings.Split(spec.args[i+1], ":"); len(p) >= 2 {
					targets = append(targets, p[1])
				}
			case "-p":
				ports = append(ports, spec.args[i+1])
			}
		}
		if !reflect.DeepEqual(sorted(targets), sorted(c.targets)) {
			t.Errorf("%s: volúmenes distintos\n  podman:  %v\n  compose: %v", name, sorted(targets), sorted(c.targets))
		}
		var cports []string
		for _, p := range c.ports {
			cports = append(cports, subst.Replace(p))
		}
		if !reflect.DeepEqual(sorted(ports), sorted(cports)) {
			t.Errorf("%s: puertos distintos: podman %v, compose %v", name, ports, cports)
		}
		if !reflect.DeepEqual(sorted(spec.caps), sorted(c.caps)) {
			t.Errorf("%s: capacidades distintas: podman %v, compose %v", name, spec.caps, c.caps)
		}
		if len(spec.cmd) > 0 {
			for _, w := range spec.cmd {
				if w != "" && !strings.Contains(c.command, w) {
					t.Errorf("%s: el comando de podman lleva %q y el del compose no (%s)", name, w, c.command)
				}
			}
		} else if c.command != "" {
			t.Errorf("%s: el compose fija un comando (%s) y podman no", name, c.command)
		}
		// el endurecimiento que podman aplica a todos debe estar en cada servicio del compose
		if !c.noCore || !c.dropAll || !c.noNewP {
			t.Errorf("%s: en el compose falta endurecimiento (core=0: %v, cap_drop ALL: %v, no-new-privileges: %v)", name, c.noCore, c.dropAll, c.noNewP)
		}
	}
	// el único puerto publicado, y solo en loopback
	for name, c := range compose {
		for _, p := range c.ports {
			if name != "app" || !strings.HasPrefix(p, "127.0.0.1:") {
				t.Errorf("%s publica %q: solo la app puede publicar, y solo en 127.0.0.1", name, p)
			}
		}
	}
}
