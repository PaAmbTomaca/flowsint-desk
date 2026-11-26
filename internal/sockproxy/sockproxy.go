// Package sockproxy es un proxy filtrado delante del socket del Docker
// rootless de la sesión. Es lo que se monta en api/celery como
// /var/run/docker.sock en lugar del socket real.
//
// Por qué: los enrichers de Flowsint (subfinder, httpx, naabu, dnsx, asnmap,
// mapcidr) lanzan contenedores con el SDK de Docker. Con el socket real, un
// RCE en la API (o un enricher manipulado) puede crear un contenedor con
// `-v $HOME:/host` y leer todo lo que el usuario puede leer, o inspeccionar
// los contenedores de las BD y sacar sus contraseñas del entorno.
//
// Qué deja pasar (lo que usa flowsint-enrichers/src/tools/dockertool.py y nada
// más): ping/version, pull e inspección de imágenes, y crear / arrancar /
// esperar / leer logs / borrar contenedores SIN montajes del host, sin
// privilegios y sin compartir namespaces. Las operaciones sobre contenedores
// solo valen para los que se crearon a través de este proxy.
package sockproxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"regexp"
	"strings"
	"sync"
)

// Modos de funcionamiento.
const (
	ModeFiltered = "filtrado" // solo lo que necesitan los enrichers
	ModeDirect   = "directo"  // sin proxy: se monta el socket real (comportamiento antiguo)
	ModeOff      = "no"       // se deniega todo: los enrichers con Docker no funcionan
)

// ValidMode dice si m es un modo conocido.
func ValidMode(m string) bool { return m == ModeFiltered || m == ModeDirect || m == ModeOff }

type Proxy struct {
	ln      net.Listener
	srv     *http.Server
	denyAll bool
	logf    func(string)

	mu  sync.Mutex
	ids map[string]bool // contenedores creados a través del proxy
}

var (
	versionPrefix = regexp.MustCompile(`^/v[0-9]+\.[0-9]+`)
	containerOp   = regexp.MustCompile(`^/containers/([A-Za-z0-9][A-Za-z0-9_.-]*)(/(start|wait|logs|json|attach|stop|kill))?$`)
	imageJSON     = regexp.MustCompile(`^/images/[^?]+/json$`)
)

// Start escucha en listenPath y reenvía a upstreamSock lo permitido. Con
// denyAll=true no reenvía nada (modo "no"). logf recibe una línea por cada
// petición denegada (sin cuerpos: pueden llevar datos).
func Start(listenPath, upstreamSock string, denyAll bool, logf func(string)) (*Proxy, error) {
	_ = os.Remove(listenPath)
	ln, err := net.Listen("unix", listenPath)
	if err != nil {
		return nil, err
	}
	// El directorio de ejecución es 0700: desde el host solo entra el usuario.
	// Dentro del contenedor el proceso corre con otro uid (USER flowsint), así
	// que el socket tiene que ser accesible para "otros".
	if err := os.Chmod(listenPath, 0o666); err != nil {
		_ = ln.Close()
		return nil, err
	}
	if logf == nil {
		logf = func(string) {}
	}
	p := &Proxy{ln: ln, denyAll: denyAll, logf: logf, ids: map[string]bool{}}
	rp := &httputil.ReverseProxy{
		Director: func(r *http.Request) {
			r.URL.Scheme = "http"
			r.URL.Host = "docker"
		},
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", upstreamSock)
			},
		},
		FlushInterval:  -1, // logs y pulls son streams
		ModifyResponse: p.rememberCreated,
	}
	p.srv = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if reason := p.check(r); reason != "" {
			p.logf(fmt.Sprintf("socket Docker: DENEGADO %s %s (%s)", r.Method, r.URL.Path, reason))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]string{"message": "flowsint-desk: operación de Docker no permitida: " + reason})
			return
		}
		rp.ServeHTTP(w, r)
	})}
	go func() { _ = p.srv.Serve(ln) }()
	return p, nil
}

// Close deja de escuchar y borra el socket.
func (p *Proxy) Close() {
	if p == nil {
		return
	}
	_ = p.srv.Close()
	_ = os.Remove(p.ln.Addr().String())
}

// check devuelve "" si la petición se permite, o el motivo del rechazo.
// Si es una creación de contenedor, valida el cuerpo y lo deja releíble.
func (p *Proxy) check(r *http.Request) string {
	if p.denyAll {
		return "el acceso a Docker está desactivado (docker_socket = \"no\")"
	}
	path := versionPrefix.ReplaceAllString(r.URL.Path, "")
	switch {
	case path == "/_ping" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		return ""
	case path == "/version" && r.Method == http.MethodGet:
		return ""
	case path == "/images/create" && r.Method == http.MethodPost:
		q := r.URL.Query()
		if q.Get("fromImage") == "" || q.Get("fromSrc") != "" {
			return "solo se permite descargar imágenes de un registro (pull)"
		}
		return ""
	case imageJSON.MatchString(path) && r.Method == http.MethodGet:
		return ""
	case path == "/containers/create" && r.Method == http.MethodPost:
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
		if err != nil {
			return "cuerpo ilegible"
		}
		if len(body) > 1<<20 {
			return "cuerpo demasiado grande"
		}
		if reason := checkCreate(body); reason != "" {
			return reason
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		return ""
	}
	if m := containerOp.FindStringSubmatch(path); m != nil {
		id, op := m[1], m[3]
		ok := (op == "" && r.Method == http.MethodDelete) ||
			((op == "json" || op == "logs") && r.Method == http.MethodGet) ||
			((op == "start" || op == "wait" || op == "attach" || op == "stop" || op == "kill") && r.Method == http.MethodPost)
		if !ok {
			return "operación no permitida"
		}
		if !p.owns(id) {
			return "ese contenedor no lo ha creado Flowsint a través de este socket"
		}
		return ""
	}
	return "endpoint fuera de la lista permitida"
}

// checkCreate valida el JSON de POST /containers/create: nada que dé acceso
// al host o a otros contenedores.
func checkCreate(body []byte) string {
	var c struct {
		HostConfig struct {
			Binds          []string
			Mounts         []json.RawMessage
			VolumesFrom    []string
			Privileged     bool
			CapAdd         []string
			Devices        []json.RawMessage
			DeviceRequests []json.RawMessage
			SecurityOpt    []string
			Sysctls        map[string]string
			NetworkMode    string
			PidMode        string
			IpcMode        string
			UTSMode        string
			UsernsMode     string
			CgroupnsMode   string
			CgroupParent   string
			Runtime        string
			Links          []string
		}
	}
	if err := json.Unmarshal(body, &c); err != nil {
		return "cuerpo JSON inválido"
	}
	h := c.HostConfig
	switch {
	case len(h.Binds) > 0 || len(h.Mounts) > 0 || len(h.VolumesFrom) > 0:
		return "no se permiten montajes (Binds/Mounts/VolumesFrom): darían acceso a ficheros del usuario"
	case h.Privileged:
		return "no se permiten contenedores privilegiados"
	case len(h.CapAdd) > 0 || len(h.Devices) > 0 || len(h.DeviceRequests) > 0 || len(h.SecurityOpt) > 0 || len(h.Sysctls) > 0:
		return "no se permiten capacidades, dispositivos, security-opt ni sysctls extra"
	case h.CgroupParent != "" || h.Runtime != "" || len(h.Links) > 0:
		return "no se permiten cgroup-parent, runtime ni links"
	}
	if n := h.NetworkMode; n != "" && n != "bridge" && n != "default" && n != "none" {
		return "red no permitida (" + n + "): solo bridge"
	}
	for nombre, v := range map[string]string{"PidMode": h.PidMode, "IpcMode": h.IpcMode, "UTSMode": h.UTSMode, "UsernsMode": h.UsernsMode, "CgroupnsMode": h.CgroupnsMode} {
		if v != "" && v != "private" {
			return nombre + " no permitido (" + v + "): no se comparten namespaces"
		}
	}
	return ""
}

// rememberCreated anota el Id de cada contenedor creado con éxito.
func (p *Proxy) rememberCreated(resp *http.Response) error {
	r := resp.Request
	if r == nil || r.Method != http.MethodPost || resp.StatusCode != http.StatusCreated ||
		versionPrefix.ReplaceAllString(r.URL.Path, "") != "/containers/create" {
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = resp.Body.Close()
	if err != nil {
		return err
	}
	var out struct{ Id string }
	if json.Unmarshal(body, &out) == nil && out.Id != "" {
		p.mu.Lock()
		p.ids[out.Id] = true
		p.mu.Unlock()
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", fmt.Sprint(len(body)))
	return nil
}

// owns: el id (completo o prefijo de 12+) corresponde a un contenedor creado
// por este proxy. Los nombres (p. ej. flowsint-desk-postgres) nunca casan.
func (p *Proxy) owns(id string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ids[id] {
		return true
	}
	if len(id) < 12 {
		return false
	}
	for full := range p.ids {
		if strings.HasPrefix(full, id) {
			return true
		}
	}
	return false
}
