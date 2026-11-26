package cli

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"flowsint-desk/internal/config"
	"flowsint-desk/internal/passphrase"
	"flowsint-desk/internal/run"
	"flowsint-desk/internal/session"

	"github.com/spf13/cobra"
)

var selftestCmd = &cobra.Command{
	Use:   "selftest",
	Short: "Prueba de humo: un ciclo completo real (arrancar, guardar, cerrar, restaurar) con un almacén desechable",
	Long: `Comprueba que TODO funciona en este equipo sin tocar tus datos, tu
configuración ni tu llavero: crea un almacén desechable en la tmpfs con una
frase aleatoria, arranca Flowsint, registra un usuario de prueba, guarda un
snapshot, cierra, vuelve a arrancar restaurando ese snapshot y comprueba que
el usuario sigue pudiendo entrar (es decir: Postgres y los secretos han
viajado bien por el snapshot cifrado). Al final lo borra todo.

Necesita que no haya ninguna sesión abierta y usa el mismo puerto y las
mismas imágenes que tu configuración.`,
	RunE: func(cmd *cobra.Command, args []string) error { return runSelftest() },
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// runSelftest ejecuta el ciclo completo lanzando este mismo binario con una
// configuración aparte (FLOWSINT_DESK_CONFIG_DIR) y sin llavero.
func runSelftest() error {
	real, err := config.Load()
	if err != nil {
		return err
	}
	runDir, err := run.Dir()
	if err != nil {
		return err
	}
	if s, err := session.Read(runDir); err == nil && s.Alive() {
		return fmt.Errorf("hay una sesión abierta (pid %d): ciérrala con `flowsint-desk down` antes de la prueba", s.PID)
	}
	if fileExists(run.Socket(runDir)) {
		return fmt.Errorf("quedan restos de una sesión anterior: ejecuta `flowsint-desk down` y repite")
	}
	if ok, err := run.IsTmpfs("/tmp"); err != nil || !ok {
		return fmt.Errorf("/tmp no es tmpfs: la prueba no escribe fuera de RAM")
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	// todo lo de la prueba en un directorio propio de la tmpfs (el directorio
	// de ejecución lo borra cada `down`, así que el almacén no puede ir dentro)
	base, err := os.MkdirTemp("/tmp", "flowsint-desk-selftest-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(base)
	vault, cfgDir := filepath.Join(base, "vault"), filepath.Join(base, "config")
	tc := *real
	tc.VaultPath, tc.Path, tc.LastForget = vault, filepath.Join(cfgDir, "config.toml"), ""
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		return err
	}
	if err := tc.Save(); err != nil {
		return err
	}
	env := append(os.Environ(), config.EnvDir+"="+cfgDir, passphrase.EnvNoKeyring+"=1")
	frase := randHex(16)
	email, clave := "selftest-"+randHex(4)+"@example.com", randHex(12)
	api := fmt.Sprintf("http://127.0.0.1:%d/api", tc.Port)

	// hijo: ejecuta `flowsint-desk <args>` con stdin dado y la salida sangrada
	hijo := func(stdin string, args ...string) *exec.Cmd {
		c := exec.Command(self, args...)
		c.Env = env
		c.Stdin = strings.NewReader(stdin)
		return c
	}
	correr := func(stdin string, args ...string) error {
		c := hijo(stdin, args...)
		out, err := c.CombinedOutput()
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			fmt.Println("    │", l)
		}
		if err != nil {
			return fmt.Errorf("`%s` falló: %w", strings.Join(args, " "), err)
		}
		return nil
	}
	// arrancar lanza `up` en segundo plano y espera a "Flowsint listo"
	arrancar := func() (*exec.Cmd, *salida, error) {
		c := hijo(frase+"\n", "up")
		pr, pw := io.Pipe()
		c.Stdout, c.Stderr = pw, pw
		if err := c.Start(); err != nil {
			return nil, nil, err
		}
		s := &salida{listo: make(chan struct{}), fin: make(chan struct{})}
		go func() { _ = c.Wait(); _ = pw.Close() }()
		go s.leer(pr)
		select {
		case <-s.listo:
			return c, s, nil
		case <-s.fin:
			return nil, nil, fmt.Errorf("`up` terminó sin llegar a arrancar")
		case <-time.After(30 * time.Minute):
			_ = c.Process.Signal(os.Interrupt)
			return nil, nil, fmt.Errorf("`up` no arrancó en 30 min")
		}
	}
	// si algo falla a medias, no dejar la sesión de prueba viva
	limpio := false
	defer func() {
		if !limpio {
			fmt.Println("· la prueba no ha terminado bien: cerrando la sesión de prueba...")
			_ = hijo("", "down").Run()
			_ = hijo("", "down").Run() // segunda orden: cerrar aunque no pueda guardar
		}
	}()
	n := 0
	hito := func(f string, a ...any) { n++; fmt.Printf("  ✔ %d. "+f+"\n", append([]any{n}, a...)...) }

	fmt.Println("· prueba de humo con un almacén desechable en", base)
	if err := correr(frase+"\n"+frase+"\n", "init", "--vault", vault); err != nil {
		return err
	}
	hito("almacén cifrado creado")
	_, s1, err := arrancar()
	if err != nil {
		return err
	}
	hito("Flowsint arranca en %s", strings.TrimSuffix(api, "/api"))
	if err := apiRegister(api, email, clave); err != nil {
		return fmt.Errorf("registrando el usuario de prueba: %w", err)
	}
	if err := apiLogin(api, email, clave); err != nil {
		return fmt.Errorf("entrando con el usuario de prueba: %w", err)
	}
	hito("la API responde: usuario de prueba creado y con acceso")
	if err := correr("", "backup"); err != nil {
		return err
	}
	hito("snapshot guardado")
	if err := correr("", "down"); err != nil {
		return err
	}
	<-s1.fin
	if _, err := os.Stat(runDir); !os.IsNotExist(err) {
		return fmt.Errorf("tras `down` el directorio de ejecución %s sigue existiendo", runDir)
	}
	hito("cierre limpio: no queda nada en el directorio de ejecución")
	_, s2, err := arrancar()
	if err != nil {
		return err
	}
	if !s2.contiene("manifest verificado") {
		return fmt.Errorf("el segundo arranque no restauró ni verificó el snapshot")
	}
	hito("segundo arranque: snapshot restaurado y verificado contra su manifest")
	if err := apiLogin(api, email, clave); err != nil {
		return fmt.Errorf("el usuario de prueba NO puede entrar tras restaurar (¿Postgres o AUTH_SECRET no viajaron?): %w", err)
	}
	hito("los datos han sobrevivido: el usuario de prueba entra tras restaurar")
	if err := correr("", "down"); err != nil {
		return err
	}
	<-s2.fin
	limpio = true
	hito("cierre final limpio")
	fmt.Println("✔ prueba de humo superada: este equipo arranca, guarda, cierra y restaura correctamente")
	return nil
}

// salida recoge lo que imprime un `up` hijo, lo muestra sangrado y avisa
// cuando aparece la línea de "listo" y cuando el proceso termina.
type salida struct {
	mu     sync.Mutex
	lineas []string
	listo  chan struct{}
	fin    chan struct{}
}

func (s *salida) leer(r io.Reader) {
	avisado := false
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		l := sc.Text()
		fmt.Println("    │", l)
		s.mu.Lock()
		s.lineas = append(s.lineas, l)
		s.mu.Unlock()
		if !avisado && strings.Contains(l, "Flowsint listo") {
			avisado = true
			close(s.listo)
		}
	}
	close(s.fin)
}

func (s *salida) contiene(txt string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, l := range s.lineas {
		if strings.Contains(l, txt) {
			return true
		}
	}
	return false
}

var apiClient = &http.Client{Timeout: 30 * time.Second}

// apiRegister crea un usuario (POST /auth/register, JSON).
func apiRegister(api, email, pass string) error {
	body, _ := json.Marshal(map[string]string{"email": email, "password": pass})
	resp, err := apiClient.Post(api+"/auth/register", "application/json", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// apiLogin pide un token (POST /auth/token, formulario) y exige access_token.
func apiLogin(api, email, pass string) error {
	resp, err := apiClient.PostForm(api+"/auth/token", url.Values{"username": {email}, "password": {pass}})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if json.Unmarshal(b, &out) != nil || out.AccessToken == "" {
		return fmt.Errorf("la respuesta no trae access_token")
	}
	return nil
}
