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
// privilegios, sin publicar puertos y sin compartir namespaces ni la red del
// stack. Además cada contenedor se crea sin capacidades y con topes. Las operaciones sobre contenedores
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
	limits  Limits
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
func Start(listenPath, upstreamSock string, denyAll bool, limits Limits, logf func(string)) (*Proxy, error) {
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
	p := &Proxy{ln: ln, denyAll: denyAll, limits: limits, logf: logf, ids: map[string]bool{}}
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
	// nada de rutas "ingeniosas": ni .. ni barras codificadas, que el proxy y
	// el motor podrían interpretar de forma distinta
	if raw := strings.ToLower(r.URL.EscapedPath()); strings.Contains(r.URL.Path, "..") ||
		strings.Contains(raw, "%2f") || strings.Contains(raw, "%5c") || strings.Contains(r.URL.Path, "//") {
		return "ruta no permitida"
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
		body, err = harden(body, p.limits)
		if err != nil {
			return "cuerpo JSON inválido"
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		r.Header.Set("Content-Length", fmt.Sprint(len(body)))
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

// Claves de HostConfig que un enricher puede fijar a un valor NO vacío. Todo
// lo demás debe venir vacío/cero: lista de lo PERMITIDO, no de lo prohibido,
// para que una opción nueva del motor no sea un agujero por omisión.
var hostConfigAllowed = map[string]bool{
	"NetworkMode": true, "AutoRemove": true, "Init": true, "ReadonlyRootfs": true,
	"Memory": true, "MemorySwap": true, "MemoryReservation": true, "MemorySwappiness": true,
	"NanoCpus": true, "CpuShares": true, "CpuPeriod": true, "CpuQuota": true, "PidsLimit": true,
	"ShmSize": true, "Ulimits": true, "Dns": true, "DnsOptions": true, "DnsSearch": true,
	"ExtraHosts": true, "ConsoleSize": true, "LogConfig": true, "Tmpfs": true,
	"RestartPolicy": true, "CapDrop": true, "MaskedPaths": true, "ReadonlyPaths": true,
	"PidMode": true, "IpcMode": true, "UTSMode": true, "UsernsMode": true, "CgroupnsMode": true,
	"Isolation": true, "OomScoreAdj": true,
}

// emptyJSON: el valor es "nada" (null, false, 0, "", [] o {}).
func emptyJSON(v json.RawMessage) bool {
	switch strings.Join(strings.Fields(string(v)), "") {
	case "", "null", "false", "0", `""`, "[]", "{}":
		return true
	}
	return false
}

// checkCreate valida el JSON de POST /containers/create: nada que dé acceso
// al host, a la red local o a otros contenedores.
func checkCreate(body []byte) string {
	var c struct {
		HostConfig       map[string]json.RawMessage
		NetworkingConfig struct {
			EndpointsConfig map[string]json.RawMessage
		}
	}
	if err := json.Unmarshal(body, &c); err != nil {
		return "cuerpo JSON inválido"
	}
	for k, v := range c.HostConfig {
		if !hostConfigAllowed[k] && !emptyJSON(v) {
			return "opción no permitida para un enricher: HostConfig." + k
		}
	}
	str := func(k string) string {
		var v string
		_ = json.Unmarshal(c.HostConfig[k], &v)
		return v
	}
	if n := str("NetworkMode"); n != "" && n != "bridge" && n != "default" && n != "none" {
		return "red no permitida (" + n + "): solo bridge"
	}
	for _, k := range []string{"PidMode", "IpcMode", "UTSMode", "UsernsMode", "CgroupnsMode"} {
		if v := str(k); v != "" && v != "private" {
			return k + " no permitido (" + v + "): no se comparten namespaces"
		}
	}
	if rp := c.HostConfig["RestartPolicy"]; !emptyJSON(rp) {
		var r struct{ Name string }
		_ = json.Unmarshal(rp, &r)
		if r.Name != "" && r.Name != "no" {
			return "no se permiten contenedores que se reinicien solos"
		}
	}
	// nada de engancharse a la red del stack (ahí está Redis sin contraseña)
	for net := range c.NetworkingConfig.EndpointsConfig {
		if net != "bridge" && net != "default" && net != "podman" {
			return "red no permitida (" + net + "): un enricher no entra en la red de Flowsint"
		}
	}
	return ""
}

// Limits son los topes que se imponen a cada contenedor de enricher. Cero =
// no imponer (p. ej. si el sistema no delega el controlador de memoria).
type Limits struct {
	MemoryBytes int64
	Pids        int64
}

// harden reescribe el cuerpo de un create YA validado para que el contenedor
// arranque con lo mínimo: sin capacidades, sin poder ganar privilegios y, si
// se puede, con tope de memoria y de procesos (la RAM es también donde vive
// toda la sesión: un enricher desbocado no debe poder llenarla).
func harden(body []byte, lim Limits) ([]byte, error) {
	var c map[string]json.RawMessage
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, err
	}
	hc := map[string]json.RawMessage{}
	if raw, ok := c["HostConfig"]; ok && !emptyJSON(raw) {
		if err := json.Unmarshal(raw, &hc); err != nil {
			return nil, err
		}
	}
	hc["CapDrop"] = json.RawMessage(`["ALL"]`)
	hc["SecurityOpt"] = json.RawMessage(`["no-new-privileges"]`)
	hc["Privileged"] = json.RawMessage(`false`)
	if lim.MemoryBytes > 0 && emptyJSON(hc["Memory"]) {
		hc["Memory"] = json.RawMessage(fmt.Sprint(lim.MemoryBytes))
	}
	if lim.Pids > 0 && emptyJSON(hc["PidsLimit"]) {
		hc["PidsLimit"] = json.RawMessage(fmt.Sprint(lim.Pids))
	}
	out, err := json.Marshal(hc)
	if err != nil {
		return nil, err
	}
	c["HostConfig"] = out
	return json.Marshal(c)
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
