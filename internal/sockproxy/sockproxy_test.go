package sockproxy

import (
	"context"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

const fakeID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// daemon de mentira: responde a todo y anota lo que le llega
func fakeDaemon(t *testing.T, sock string, seen *[]string) {
	t.Helper()
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.Method+" "+r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/containers/create") {
			b, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(b), "projectdiscovery") {
				t.Errorf("el cuerpo de create no llegó entero al daemon: %q", b)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"Id":"` + fakeID + `","Warnings":[]}`))
			return
		}
		_, _ = w.Write([]byte("OK"))
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
}

func client(sock string) *http.Client {
	return &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", sock)
	}}}
}

func do(t *testing.T, c *http.Client, method, path, body string) int {
	t.Helper()
	req, _ := http.NewRequest(method, "http://docker"+path, strings.NewReader(body))
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
	return resp.StatusCode
}

func TestFlujoDeEnricherPermitidoYLoDemasNo(t *testing.T) {
	d := t.TempDir()
	up, px := filepath.Join(d, "real.sock"), filepath.Join(d, "proxy.sock")
	var seen []string
	fakeDaemon(t, up, &seen)
	var denegados []string
	p, err := Start(px, up, false, func(s string) { denegados = append(denegados, s) })
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	c := client(px)

	// lo que hace dockertool.py: ping, version, pull, inspect, create, start, wait, logs, delete
	okCreate := `{"Image":"projectdiscovery/subfinder:latest","Cmd":["-d","sonda.example"],"HostConfig":{"NetworkMode":"bridge","Binds":[]}}`
	permitidos := [][3]string{
		{"GET", "/_ping", ""},
		{"GET", "/v1.43/version", ""},
		{"POST", "/v1.43/images/create?fromImage=projectdiscovery%2Fsubfinder&tag=latest", ""},
		{"GET", "/v1.43/images/projectdiscovery/subfinder:latest/json", ""},
		{"POST", "/v1.43/containers/create", okCreate},
		{"POST", "/v1.43/containers/" + fakeID + "/start", ""},
		{"POST", "/v1.43/containers/" + fakeID + "/wait", ""},
		{"GET", "/v1.43/containers/" + fakeID + "/logs", ""},
		{"GET", "/v1.43/containers/" + fakeID[:12] + "/json", ""},
		{"DELETE", "/v1.43/containers/" + fakeID, ""},
	}
	for _, r := range permitidos {
		if code := do(t, c, r[0], r[1], r[2]); code >= 400 {
			t.Errorf("debería permitirse %s %s → %d", r[0], r[1], code)
		}
	}
	antes := len(seen)
	denegar := [][3]string{
		// leer el home del usuario
		{"POST", "/v1.43/containers/create", `{"Image":"alpine","HostConfig":{"Binds":["/home/pollo:/host"]}}`},
		{"POST", "/v1.43/containers/create", `{"Image":"alpine","HostConfig":{"Mounts":[{"Type":"bind","Source":"/","Target":"/host"}]}}`},
		{"POST", "/v1.43/containers/create", `{"Image":"alpine","HostConfig":{"VolumesFrom":["flowsint-desk-postgres"]}}`},
		{"POST", "/v1.43/containers/create", `{"Image":"alpine","HostConfig":{"Privileged":true}}`},
		{"POST", "/v1.43/containers/create", `{"Image":"alpine","HostConfig":{"NetworkMode":"container:flowsint-desk-postgres"}}`},
		{"POST", "/v1.43/containers/create", `{"Image":"alpine","HostConfig":{"NetworkMode":"host"}}`},
		{"POST", "/v1.43/containers/create", `{"Image":"alpine","HostConfig":{"PidMode":"host"}}`},
		{"POST", "/v1.43/containers/create", `{"Image":"alpine","HostConfig":{"CapAdd":["SYS_ADMIN"]}}`},
		{"POST", "/v1.43/containers/create", `no es json`},
		// espiar o tocar los contenedores de las BD (no creados por el proxy)
		{"GET", "/v1.43/containers/flowsint-desk-postgres/json", ""},
		{"GET", "/v1.43/containers/flowsint-desk-neo4j/logs", ""},
		{"DELETE", "/v1.43/containers/flowsint-desk-postgres", ""},
		{"POST", "/v1.43/containers/flowsint-desk-postgres/exec", ""},
		{"POST", "/v1.43/containers/" + fakeID + "/exec", ""},
		{"GET", "/v1.43/containers/" + fakeID + "/archive", ""},
		// todo lo demás
		{"GET", "/v1.43/containers/json", ""},
		{"GET", "/v1.43/volumes", ""},
		{"POST", "/v1.43/volumes/create", ""},
		{"GET", "/v1.43/images/json", ""},
		{"POST", "/v1.43/images/create?fromSrc=-", ""},
		{"POST", "/v1.43/build", ""},
		{"GET", "/v1.43/info", ""},
		{"GET", "/v1.43/secrets", ""},
	}
	for _, r := range denegar {
		if code := do(t, c, r[0], r[1], r[2]); code != http.StatusForbidden {
			t.Errorf("debería DENEGARSE %s %s %s → %d", r[0], r[1], r[2], code)
		}
	}
	if len(seen) != antes {
		t.Errorf("peticiones denegadas llegaron al daemon: %v", seen[antes:])
	}
	if len(denegados) != len(denegar) {
		t.Errorf("se esperaban %d líneas de denegación, hay %d", len(denegar), len(denegados))
	}
	for _, l := range denegados {
		if strings.Contains(l, "/home/pollo") {
			t.Error("el log de denegaciones no debe incluir cuerpos")
		}
	}
}

func TestModoNoDeniegaTodo(t *testing.T) {
	d := t.TempDir()
	up, px := filepath.Join(d, "real.sock"), filepath.Join(d, "proxy.sock")
	var seen []string
	fakeDaemon(t, up, &seen)
	p, err := Start(px, up, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if code := do(t, client(px), "GET", "/_ping", ""); code != http.StatusForbidden {
		t.Fatalf("modo no: /_ping → %d", code)
	}
	if len(seen) != 0 {
		t.Fatal("en modo no, nada debe llegar al daemon")
	}
}

func TestValidMode(t *testing.T) {
	for _, m := range []string{ModeFiltered, ModeDirect, ModeOff} {
		if !ValidMode(m) {
			t.Errorf("%q debería valer", m)
		}
	}
	if ValidMode("") || ValidMode("abierto") {
		t.Error("modos desconocidos no valen")
	}
}
