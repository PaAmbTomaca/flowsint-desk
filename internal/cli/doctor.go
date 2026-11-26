package cli

import (
	"bytes"
	"compress/bzip2"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"flowsint-desk/internal/config"
	"flowsint-desk/internal/lock"
	"flowsint-desk/internal/passphrase"
	"flowsint-desk/internal/run"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

var doctorFix, doctorPackages bool

// doctorCheck es una comprobación del entorno. Si falla y warn=true solo
// avisa (no suma a los errores). fix es el paso exacto para arreglarlo a
// mano; auto, si no es nil, lo intenta solo con doctor --fix (nunca sudo).
type doctorCheck struct {
	name string
	warn bool
	run  func() (bool, string) // ok, detalle (se imprime entre paréntesis)
	fix  string
	// afterInit: lo resuelve el paso de almacén del asistente (`setup`/`init`),
	// no un comando del sistema.
	afterInit bool
	auto      func() (string, error) // devuelve una nota opcional
}

var doctorCmd = &cobra.Command{
	Use:   "doctor [--fix]",
	Short: "Comprueba TODOS los requisitos del entorno y guía para configurarlos",
	Long: `Comprueba cada requisito de flowsint-desk y, si falta algo, imprime el paso
exacto para arreglarlo. Con --fix intenta arreglar automáticamente (sin sudo)
lo instalable: descarga restic (verificando su sha256), los binarios rootless
(rootlesskit, slirp4netns, fuse-overlayfs, dockerd-rootless.sh) a ~/.local/bin,
y crea la caché de imágenes y la configuración por defecto. Lo que requiera
sudo (subuid/subgid, montar /tmp, sysctls) solo se imprime.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if doctorPackages {
			c := allPackagesCmd(currentFamily())
			if c == "" {
				return fmt.Errorf("distribución no reconocida: instala con tu gestor de paquetes docker (con dockerd y compose v2), newuidmap/newgidmap, rootlesskit, slirp4netns, fuse-overlayfs y restic")
			}
			fmt.Println(c)
			return nil
		}
		_, err := runDoctor(doctorFix)
		return err
	},
}

// runDoctor ejecuta todos los chequeos; con fix=true intenta arreglar
// automáticamente los que no requieran sudo. Devuelve los que siguen en rojo.
func runDoctor(fix bool) ([]*doctorCheck, error) {
	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		// doctor tiene que poder diagnosticar una config rota, no morir con ella
		fmt.Printf("✖ configuración inválida (%v)\n   → arreglar: corrige el fichero o bórralo para volver a los valores por defecto\n", cfgErr)
		cfg = config.Default()
		cfg.Path, _ = config.FilePath()
	}
	username := os.Getenv("USER")
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
	fail := 0
	if cfgErr != nil {
		fail++
	}
	var pendientes []*doctorCheck
	for _, c := range doctorChecks(cfg, username) {
		ok, detail := c.run()
		if ok {
			printCheck("✔", c.name, detail)
			continue
		}
		mark := "✖"
		if c.warn {
			mark = "⚠"
		} else {
			fail++
		}
		printCheck(mark, c.name, detail)
		fmt.Printf("   → arreglar: %s\n", c.fix)
		if !c.warn {
			if fix && c.auto != nil {
				nota, err := c.auto()
				if err != nil {
					fmt.Printf("   ⚠ auto-fix falló: %v\n", err)
				} else if ok2, d2 := c.run(); ok2 {
					fail--
					if nota != "" {
						fmt.Printf("   ✔ arreglado automáticamente (%s)\n", nota)
					} else {
						fmt.Printf("   ✔ arreglado automáticamente\n")
					}
					printCheck("✔", c.name, d2)
					continue
				} else if nota != "" {
					fmt.Printf("   ⚠ sigue en rojo tras el auto-fix (%s)\n", nota)
				}
			}
			pendientes = append(pendientes, c)
		}
	}
	if fail > 0 {
		fmt.Printf("\n✖ %d comprobaciones en rojo", fail)
		if !fix {
			fmt.Print(" — prueba `flowsint-desk setup` (o `doctor --fix`) para los arreglos automáticos")
		}
		fmt.Println()
		return pendientes, fmt.Errorf("%d comprobaciones en rojo", fail)
	}
	fmt.Println("\n✔ todos los requisitos OK")
	vault := cfg.VaultPath
	if st, err := os.Stat(filepath.Join(vault, "config")); err == nil && !st.IsDir() {
		fmt.Println("→ siguiente paso: flowsint-desk up")
	} else {
		fmt.Println("→ siguiente paso: flowsint-desk init --vault " + vault)
	}
	return pendientes, nil
}

func printCheck(mark, name, detail string) {
	if detail != "" {
		fmt.Printf("%s %s (%s)\n", mark, name, detail)
	} else {
		fmt.Printf("%s %s\n", mark, name)
	}
}

func doctorChecks(cfg *config.Config, username string) []*doctorCheck {
	home, _ := os.UserHomeDir()
	cacheDir := filepath.Join(home, ".cache", "flowsint-desk", "images")
	vault := cfg.VaultPath
	host, _ := os.Hostname()

	look := func(b string) func() (bool, string) {
		return func() (bool, string) {
			p, err := exec.LookPath(b)
			if err != nil {
				return false, ""
			}
			return true, p
		}
	}
	fam := currentFamily()
	// Los binarios rootless solo se descargan solos en Arch (paquetes del
	// mirror de Arch); en el resto se indica el paquete del sistema.
	rootlessBin := func(bin string) *doctorCheck {
		c := &doctorCheck{name: "binario " + bin, run: look(bin)}
		if fam == famArch {
			c.fix = "`flowsint-desk setup` lo descarga a ~/.local/bin (o: " + installHint(fam, bin) + ")"
			c.auto = autoArchBin(bin)
		} else {
			c.fix = installHint(fam, bin)
		}
		return c
	}

	return []*doctorCheck{
		// --- binarios rootless ---
		rootlessBin("rootlesskit"),
		rootlessBin("slirp4netns"),
		rootlessBin("fuse-overlayfs"),
		{name: "binario dockerd-rootless.sh", run: look("dockerd-rootless.sh"),
			fix:  "`flowsint-desk setup` lo descarga a ~/.local/bin (es un script de moby, vale para cualquier distribución)",
			auto: autoDockerdScript},
		{name: "binario newuidmap", run: look("newuidmap"), fix: installHint(fam, "newuidmap")},
		{name: "binario newgidmap", run: look("newgidmap"), fix: installHint(fam, "newuidmap")},

		// --- mapeo de uids y user namespaces ---
		{name: "/etc/subuid tiene entrada para " + username,
			run: func() (bool, string) { return fileHasLine("/etc/subuid", username+":") },
			fix: "sudo sh -c \"echo '" + username + ":100000:65536' >> /etc/subuid\""},
		{name: "/etc/subgid tiene entrada para " + username,
			run: func() (bool, string) { return fileHasLine("/etc/subgid", username+":") },
			fix: "sudo sh -c \"echo '" + username + ":100000:65536' >> /etc/subgid\""},
		{name: "user namespaces habilitados (user.max_user_namespaces > 0)",
			run: func() (bool, string) {
				b, err := os.ReadFile("/proc/sys/user/max_user_namespaces")
				if err != nil {
					return false, ""
				}
				n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
				return n > 0, strings.TrimSpace(string(b))
			},
			fix: "sudo sysctl -w user.max_user_namespaces=100000 (persistente: /etc/sysctl.d/99-flowint-desk.conf)"},
		{name: "kernel ≥ 5.11 (rootless docker moderno)", warn: true,
			run: func() (bool, string) {
				rel := kernelRelease()
				maj, min := parseKernel(rel)
				return maj > 5 || (maj == 5 && min >= 11), rel
			},
			fix: "actualiza el kernel de la distribución"},

		// --- restic, docker, compose ---
		{name: "restic instalado y ≥ 0.17",
			run: func() (bool, string) {
				out, err := exec.Command("restic", "version").CombinedOutput()
				if err != nil {
					return false, ""
				}
				v := strings.Fields(string(out))
				if len(v) < 2 {
					return false, ""
				}
				maj, min, ok := parseVersion(strings.TrimPrefix(v[1], "v"))
				return ok && (maj > 0 || min >= 17), strings.TrimSpace(string(out))
			},
			fix:  "`flowsint-desk setup` descarga el binario oficial (sha256 verificado) a ~/.local/bin; o: " + installHint(fam, "restic"),
			auto: autoRestic},
		{name: "binario docker", run: look("docker"),
			fix: installHint(fam, "docker") + " (solo se usan el cliente, dockerd y compose; el daemon del sistema NO se toca)"},
		{name: "docker compose v2",
			run: func() (bool, string) {
				out, err := exec.Command("docker", "compose", "version").CombinedOutput()
				if err != nil {
					return false, ""
				}
				return true, strings.TrimSpace(string(out))
			},
			fix: installHint(fam, "compose") + " (o el plugin docker-compose-plugin del repositorio oficial de Docker)"},

		// --- tmpfs, RAM, puerto ---
		{name: "/tmp es tmpfs (statfs TMPFS_MAGIC 0x01021994)",
			run: func() (bool, string) {
				ok, err := run.IsTmpfs("/tmp")
				return err == nil && ok, ""
			},
			fix: "monta /tmp como tmpfs: tmpfs /tmp tmpfs rw,noatime,size=16G en /etc/fstab"},
		{name: "espacio libre en la tmpfs de /tmp ≥ 8 GiB",
			run: func() (bool, string) {
				f := freeBytes("/tmp")
				return f >= 8<<30, fmt.Sprintf("%.1f GiB libres", float64(f)/(1<<30))
			},
			fix: "amplia la tmpfs de /tmp (size=... en /etc/fstab) o libera espacio: BDs e imágenes viven ahí mientras la app está abierta"},
		{name: "swap solo en RAM (zram) — tmpfs no puede acabar en disco", warn: true,
			run: func() (bool, string) {
				b, err := os.ReadFile("/proc/swaps")
				if err != nil {
					return true, "no se pudo leer /proc/swaps"
				}
				disco := nonZramSwaps(string(b))
				if len(disco) == 0 {
					return true, "sin swap en disco"
				}
				return false, "swap fuera de RAM: " + strings.Join(disco, ", ")
			},
			fix: "el kernel puede mandar páginas de /tmp (tmpfs) al swap: si ese swap está en disco SIN cifrar, los datos de la sesión pueden quedar en claro en el disco. Usa solo zram, cifra el swap (dm-crypt con clave aleatoria) o desactívalo mientras trabajas (sudo swapoff <dispositivo>). Lo mismo vale para hibernar (suspend-to-disk) con la sesión abierta: ciérrala antes"},
		// SELinux en modo enforcing (Fedora/RHEL de fábrica) bloquea el acceso de
		// los contenedores a ficheros del host montados sin etiquetar
		{name: "SELinux no bloquea los montajes de los contenedores", warn: true,
			run: func() (bool, string) {
				b, err := os.ReadFile("/sys/fs/selinux/enforce")
				if err != nil {
					return true, "sin SELinux"
				}
				if strings.TrimSpace(string(b)) == "1" {
					return false, "SELinux en modo enforcing"
				}
				return true, "SELinux permisivo"
			},
			fix: "flowsint-desk monta en los contenedores un socket y un directorio de la tmpfs sin etiqueta SELinux: con enforcing es probable que Flowsint no pueda usarlos (enrichers, import de Neo4j). NO está probado en Fedora: ejecuta `flowsint-desk selftest` y, si falla por permisos, mira `sudo ausearch -m avc -ts recent`"},
		{name: "RAM libre ≥ 4 GB",
			run: func() (bool, string) {
				mem := readMemAvailable()
				return mem >= 4<<30, fmt.Sprintf("MemAvailable=%d MiB", mem>>20)
			},
			fix: "libera memoria o reduce el tamaño de la tmpfs de /tmp"},
		{name: fmt.Sprintf("puerto 127.0.0.1:%d libre", cfg.Port),
			run: func() (bool, string) {
				ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.Port))
				if err != nil {
					return false, ""
				}
				_ = ln.Close()
				return true, ""
			},
			fix: "cambia el puerto en ~/.config/flowsint-desk/config.toml (port = ...) o libera el puerto"},

		// --- almacén (pCloud) ---
		{name: "almacén existe y es escribible: " + vault, afterInit: true,
			run: func() (bool, string) {
				if _, err := os.Stat(vault); err != nil {
					return false, "no existe"
				}
				return unix.Access(vault, unix.W_OK) == nil, ""
			},
			fix: "abre pCloud Drive (debe estar montado y con sesión iniciada) y ejecuta `flowsint-desk setup` o `flowsint-desk init --vault " + vault + "`"},
		{name: "pCloud Drive montado", warn: true,
			run: func() (bool, string) {
				m := detectPCloud()
				return m != "", m
			},
			fix: "arranca el cliente de pCloud e inicia sesión: sin él montado no se puede leer ni guardar el almacén"},
		{name: "almacén sobre el montaje FUSE de pCloud", warn: true,
			run: func() (bool, string) {
				typ, mnt := fuseMountOf(vault)
				if typ == "" {
					return false, "sin montaje FUSE detectado"
				}
				return strings.Contains(typ, "fuse") || strings.Contains(typ, "pcloud"), mnt + " tipo " + typ
			},
			fix: "comprueba que " + vault + " queda dentro del punto de montaje de pCloud (p. ej. " + filepath.Join(home, "pCloudDrive") + ")"},
		{name: "repositorio restic inicializado en el almacén", warn: true,
			run: func() (bool, string) {
				_, err := os.Stat(filepath.Join(vault, "config"))
				return err == nil, ""
			},
			fix: "flowsint-desk init --vault " + vault},
		{name: "bloqueo del almacén", warn: true,
			run: func() (bool, string) {
				l, err := lock.Read(vault)
				if err != nil {
					return true, "sin bloqueo activo"
				}
				hb, _ := time.Parse(time.RFC3339, l.Heartbeat)
				if lock.IsLocal(l, host) {
					return true, fmt.Sprintf("de esta máquina (pid %d)", l.PID)
				}
				if time.Since(hb) < lock.StaleMinutes*time.Minute {
					return false, fmt.Sprintf("bloqueado por %s, latido hace %s", l.Host, time.Since(hb).Round(time.Second))
				}
				return false, fmt.Sprintf("CADUCADO de %s, latido hace %s", l.Host, time.Since(hb).Round(time.Second))
			},
			fix: "si está vivo, espera a que el otro equipo cierre; si está caducado: flowsint-desk unlock --force"},

		// --- configuración local (sin secretos) ---
		{name: "config.toml con permisos restrictivos (0600)",
			run: func() (bool, string) {
				st, err := os.Stat(cfg.Path)
				if os.IsNotExist(err) {
					return true, "se creará con init / doctor --fix"
				}
				if err != nil {
					return false, ""
				}
				return st.Mode().Perm()&0o077 == 0, fmt.Sprintf("modo %04o", st.Mode().Perm())
			},
			fix:  "chmod 600 " + cfg.Path,
			auto: autoConfigPerms},
		{name: "config.toml sin claves con pinta de secreto (nunca se imprimen valores)",
			run: func() (bool, string) {
				bad := secretishKey(cfg.Path)
				if bad != "" {
					return false, "clave sospechosa: " + bad
				}
				return true, ""
			},
			fix: "quita esa clave de " + cfg.Path + "; los secretos solo viajan en la frase de paso / llavero / snapshot cifrado"},

		// --- llavero, apagado, caché, red ---
		{name: "llavero Secret Service disponible (frase opcional en llavero)", warn: true,
			run: func() (bool, string) { return passphrase.KeyringAvailable(), "" },
			fix: "necesita sesión de escritorio con D-Bus: export DBUS_SESSION_BUS_ADDRESS=unix:path=$XDG_RUNTIME_DIR/bus y gnome-keyring/libsecret; si no, la frase simplement se pedirá cada vez (vale igual)"},
		{name: fmt.Sprintf("logind espera ≥ %d s al apagar (snapshot final al apagar el equipo)", minShutdownWindow), warn: true,
			run: func() (bool, string) {
				w := inhibitDelaySeconds()
				if w < 0 {
					return false, "no se pudo consultar a logind (¿sin systemd?)"
				}
				return w >= minShutdownWindow, fmt.Sprintf("InhibitDelayMaxSec=%d", w)
			},
			fix: shutdownWindowAdvice()},
		{name: "caché de imágenes escribible y $HOME con ≥ 6 GiB libres",
			run: func() (bool, string) {
				if _, err := os.Stat(cacheDir); err != nil {
					return false, "no existe " + cacheDir
				}
				if unix.Access(cacheDir, unix.W_OK) != nil {
					return false, "sin permiso de escritura"
				}
				f := freeBytes(home)
				if f < 6<<30 {
					return false, fmt.Sprintf("%.1f GiB libres en %s", float64(f)/(1<<30), home)
				}
				return true, fmt.Sprintf("%.1f GiB libres en $HOME", float64(f)/(1<<30))
			},
			fix:  "libera espacio en $HOME; las imágenes públicas (~varios GiB) se cachean en ~/.cache/flowsint-desk/images",
			auto: autoCacheDir},
		// aviso, no bloqueo: con las imágenes ya en caché se trabaja sin red
		{name: "conectividad HTTPS a ghcr.io:443 y github.com:443", warn: true,
			run: func() (bool, string) {
				ok1 := canDial("ghcr.io")
				ok2 := canDial("github.com")
				return ok1 && ok2, fmt.Sprintf("ghcr.io=%v github.com=%v", ok1, ok2)
			},
			fix: "comprueba DNS, firewall y proxy (HTTPS_PROXY); se necesita para bajar imágenes de Flowsint y binarios con --fix"},
	}
}

// --- helpers de comprobación ---

func readMemAvailable() int64 {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "MemAvailable:") {
			f := strings.Fields(line)
			if len(f) >= 2 {
				kb, _ := strconv.ParseInt(f[1], 10, 64)
				return kb * 1024
			}
		}
	}
	return 0
}

// nonZramSwaps devuelve los dispositivos/ficheros de swap activos (contenido
// de /proc/swaps) que NO son zram, es decir, que pueden acabar en disco.
func nonZramSwaps(procSwaps string) []string {
	var out []string
	for i, line := range strings.Split(procSwaps, "\n") {
		f := strings.Fields(line)
		if i == 0 || len(f) == 0 { // cabecera o línea vacía
			continue
		}
		if !strings.HasPrefix(f[0], "/dev/zram") {
			out = append(out, f[0])
		}
	}
	return out
}

func freeBytes(path string) int64 {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return -1
	}
	return int64(st.Bavail) * int64(st.Bsize)
}

func fileHasLine(path, prefix string) (bool, string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, prefix) {
			return true, strings.TrimSpace(line)
		}
	}
	return false, ""
}

func kernelRelease() string {
	var u unix.Utsname
	if err := unix.Uname(&u); err != nil {
		return "?"
	}
	return charsToString(u.Release[:])
}

func charsToString(c []byte) string {
	b := make([]byte, 0, len(c))
	for _, v := range c {
		if v == 0 {
			break
		}
		b = append(b, v)
	}
	return string(b)
}

func parseKernel(rel string) (int, int) {
	parts := strings.SplitN(rel, ".", 3)
	maj, _ := strconv.Atoi(parts[0])
	min := 0
	if len(parts) > 1 {
		min, _ = strconv.Atoi(strings.SplitN(parts[1], "-", 2)[0])
	}
	return maj, min
}

func parseVersion(v string) (int, int, bool) {
	parts := strings.Split(v, ".")
	if len(parts) < 2 {
		return 0, 0, false
	}
	maj, e1 := strconv.Atoi(parts[0])
	min, e2 := strconv.Atoi(strings.SplitN(parts[1], "-", 2)[0])
	return maj, min, e1 == nil && e2 == nil
}

// fuseMountOf devuelve el tipo y el punto de montaje más largo de /proc/mounts
// que contiene a path.
func fuseMountOf(path string) (string, string) {
	b, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return "", ""
	}
	bestTyp, bestMnt := "", ""
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		mnt := strings.ReplaceAll(f[1], `\040`, " ")
		if strings.HasPrefix(path, mnt) && len(mnt) > len(bestMnt) {
			bestTyp, bestMnt = f[2], mnt
		}
	}
	return bestTyp, bestMnt
}

// secretishKey devuelve el nombre de la primera clave del TOML cuya pinta sea
// de secreto. NUNCA lee ni devuelve valores.
func secretishKey(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	suspect := []string{"pass", "secret", "token", "key", "frase", "credential"}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(strings.SplitN(line, "=", 2)[0]))
		for _, s := range suspect {
			if strings.Contains(k, s) {
				return k
			}
		}
	}
	return ""
}

func canDial(host string) bool {
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, "443"), 5*time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// --- auto-fixes (doctor --fix); ninguno usa sudo ---

func httpGet(url string) ([]byte, error) {
	c := &http.Client{Timeout: 180 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s → HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 512<<20))
}

func localBinDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	d := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return d, nil
}

func writeLocalBin(name string, data []byte) error {
	d, err := localBinDir()
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(d, name), data, 0o755)
}

// Paquetes de Arch (repo extra) que traen cada binario rootless. El nombre de
// fichero actual se pregunta a archlinux.org; estos son la reserva.
var archBinPkgs = map[string]string{
	"rootlesskit":    "rootlesskit-3.3.0-1-x86_64.pkg.tar.zst",
	"slirp4netns":    "slirp4netns-1.3.7-1-x86_64.pkg.tar.zst",
	"fuse-overlayfs": "fuse-overlayfs-1.18-1-x86_64.pkg.tar.zst",
}

const (
	archMirror  = "https://geo.mirror.pkgbuild.com/extra/os/x86_64/"
	archKeyring = "/usr/share/pacman/keyrings/archlinux.gpg"
)

// archPkgFilename extrae el nombre de fichero del paquete de la respuesta
// JSON de archlinux.org/packages/extra/x86_64/<nombre>/json/.
func archPkgFilename(js []byte, name string) (string, error) {
	var r struct {
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(js, &r); err != nil {
		return "", err
	}
	// el nombre acaba en la URL de descarga: nada de rutas ni sorpresas
	if !strings.HasPrefix(r.Filename, name+"-") || !strings.HasSuffix(r.Filename, ".pkg.tar.zst") || strings.ContainsAny(r.Filename, "/\\?#") {
		return "", fmt.Errorf("nombre de paquete inesperado: %q", r.Filename)
	}
	return r.Filename, nil
}

// autoArchBin descarga el paquete oficial de Arch, VERIFICA SU FIRMA con el
// llavero de paquetes del sistema (gpgv + archlinux-keyring) y solo entonces
// extrae el binario a ~/.local/bin. Sin gpgv o sin llavero se niega: instalar
// un binario sin verificar no compensa cuando `pacman -S` lo hace bien.
func autoArchBin(bin string) func() (string, error) {
	return func() (string, error) {
		pkg, ok := archBinPkgs[bin]
		if !ok {
			return "", fmt.Errorf("no hay paquete automático para %s", bin)
		}
		if _, err := exec.LookPath("gpgv"); err != nil {
			return "", fmt.Errorf("no hay gpgv para verificar la firma del paquete: instálalo con %s", installHint(famArch, bin))
		}
		if _, err := os.Stat(archKeyring); err != nil {
			return "", fmt.Errorf("no está el llavero de paquetes (%s): instálalo con %s", archKeyring, installHint(famArch, bin))
		}
		// versión vigente (las antiguas desaparecen del mirror al actualizarse)
		if js, err := httpGet("https://archlinux.org/packages/extra/x86_64/" + bin + "/json/"); err == nil {
			if f, err := archPkgFilename(js, bin); err == nil {
				pkg = f
			}
		}
		data, err := httpGet(archMirror + pkg)
		if err != nil {
			return "", err
		}
		sig, err := httpGet(archMirror + pkg + ".sig")
		if err != nil {
			return "", fmt.Errorf("no se pudo bajar la firma del paquete: %w", err)
		}
		tmp, err := os.MkdirTemp("", "flowsint-fix-")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(tmp)
		pf := filepath.Join(tmp, pkg)
		if err := os.WriteFile(pf, data, 0o600); err != nil {
			return "", err
		}
		if err := os.WriteFile(pf+".sig", sig, 0o600); err != nil {
			return "", err
		}
		// el llavero de Arch se distribuye en ASCII (armored) y gpgv solo lee
		// llaveros binarios: se convierte a un temporal
		kb, err := os.ReadFile(archKeyring)
		if err != nil {
			return "", err
		}
		krBin, err := dearmor(kb)
		if err != nil {
			return "", fmt.Errorf("no se pudo leer el llavero de paquetes %s: %w", archKeyring, err)
		}
		kr := filepath.Join(tmp, "arch-keyring.gpg")
		if err := os.WriteFile(kr, krBin, 0o600); err != nil {
			return "", err
		}
		if out, err := exec.Command("gpgv", "--keyring", kr, pf+".sig", pf).CombinedOutput(); err != nil {
			return "", fmt.Errorf("la FIRMA del paquete %s no es válida: no se instala (%s)", pkg, lastLine(string(out)))
		}
		if out, err := exec.Command("tar", "--zstd", "-xf", pf, "-C", tmp, "usr/bin/"+bin).CombinedOutput(); err != nil {
			return "", fmt.Errorf("tar: %v (%s)", err, strings.TrimSpace(string(out)))
		}
		b, err := os.ReadFile(filepath.Join(tmp, "usr", "bin", bin))
		if err != nil {
			return "", fmt.Errorf("el paquete no trae usr/bin/%s", bin)
		}
		if err := writeLocalBin(bin, b); err != nil {
			return "", err
		}
		return pkg + ", firma verificada con el llavero de Arch", nil
	}
}

// dearmor convierte un llavero OpenPGP en ASCII ("-----BEGIN PGP PUBLIC KEY
// BLOCK-----", uno o varios bloques) a binario. Si ya es binario lo devuelve
// tal cual.
func dearmor(b []byte) ([]byte, error) {
	if !bytes.HasPrefix(bytes.TrimSpace(b), []byte("-----BEGIN PGP")) {
		return b, nil
	}
	var out []byte
	var b64 strings.Builder
	dentro, cabeceras := false, false
	flush := func() error {
		if b64.Len() == 0 {
			return nil
		}
		dec, err := base64.StdEncoding.DecodeString(b64.String())
		if err != nil {
			return err
		}
		out = append(out, dec...)
		b64.Reset()
		return nil
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "-----BEGIN PGP"):
			dentro, cabeceras = true, true
		case strings.HasPrefix(line, "-----END PGP"):
			if err := flush(); err != nil {
				return nil, err
			}
			dentro = false
		case !dentro:
		case cabeceras:
			// cabeceras "Clave: valor" hasta la primera línea en blanco
			if line == "" {
				cabeceras = false
			} else if !strings.Contains(line, ": ") {
				cabeceras = false
				b64.WriteString(line)
			}
		case strings.HasPrefix(line, "="): // suma CRC24 del bloque: no es dato
		default:
			b64.WriteString(line)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("llavero ASCII sin datos")
	}
	return out, nil
}

func lastLine(s string) string {
	l := strings.Split(strings.TrimSpace(s), "\n")
	return l[len(l)-1]
}

// dockerVersion extrae "29.0.1" de "Docker version 29.0.1, build abc".
func dockerVersion(out string) string {
	f := strings.Fields(out)
	for i, w := range f {
		if w == "version" && i+1 < len(f) {
			v := strings.TrimRight(f[i+1], ",")
			if _, _, ok := parseVersion(v); ok && !strings.ContainsAny(v, "/\\?#") {
				return v
			}
		}
	}
	return ""
}

// autoDockerdScript consigue dockerd-rootless.sh, por orden de confianza:
// 1) el que ya trae el paquete de Docker del sistema; 2) el de moby en la
// etiqueta de la MISMA versión que el Docker instalado; 3) la rama master
// (avisando: es código sin fijar que se va a ejecutar).
func autoDockerdScript() (string, error) {
	for _, p := range []string{
		"/usr/share/docker.io/contrib/dockerd-rootless.sh",
		"/usr/share/docker/contrib/dockerd-rootless.sh",
		"/usr/libexec/docker/dockerd-rootless.sh",
	} {
		if b, err := os.ReadFile(p); err == nil {
			return "copiado de " + p + " (paquete del sistema)", writeLocalBin("dockerd-rootless.sh", b)
		}
	}
	const base = "https://raw.githubusercontent.com/moby/moby/"
	const file = "/contrib/dockerd-rootless.sh"
	if out, err := exec.Command("docker", "--version").Output(); err == nil {
		if v := dockerVersion(string(out)); v != "" {
			for _, ref := range []string{"docker-v" + v, "v" + v} {
				if data, err := httpGet(base + ref + file); err == nil {
					return "moby " + ref + " (misma versión que tu Docker)", writeLocalBin("dockerd-rootless.sh", data)
				}
			}
		}
	}
	data, err := httpGet(base + "master" + file)
	if err != nil {
		return "", err
	}
	return "moby master — AVISO: versión sin fijar; revisa ~/.local/bin/dockerd-rootless.sh o usa el del paquete de Docker de tu distribución", writeLocalBin("dockerd-rootless.sh", data)
}

func autoRestic() (string, error) {
	const ver = "0.19.1"
	const file = "restic_" + ver + "_linux_amd64.bz2"
	base := "https://github.com/restic/restic/releases/download/v" + ver + "/"
	bz, err := httpGet(base + file)
	if err != nil {
		return "", err
	}
	// restic publica UN fichero SHA256SUMS por release (no un .sha256 por binario)
	sums, err := httpGet(base + "SHA256SUMS")
	if err != nil {
		return "", err
	}
	want, err := sumFor(string(sums), file)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(bz)
	got := hex.EncodeToString(h[:])
	if got != want {
		return "", fmt.Errorf("sha256 no cuadra (descargado %s, oficial %s)", got, want)
	}
	raw, err := io.ReadAll(bzip2.NewReader(bytes.NewReader(bz)))
	if err != nil {
		return "", fmt.Errorf("bzip2: %w", err)
	}
	if err := writeLocalBin("restic", raw); err != nil {
		return "", err
	}
	return "restic v" + ver + " (sha256 verificado contra el SHA256SUMS oficial)", nil
}

// sumFor busca en un SHA256SUMS ("<hash>  <fichero>" por línea) el hash del
// fichero pedido.
func sumFor(sums, file string) (string, error) {
	for _, line := range strings.Split(sums, "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == file {
			if len(f[0]) != 64 {
				return "", fmt.Errorf("hash con formato inesperado para %s", file)
			}
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("%s no aparece en el SHA256SUMS oficial", file)
}

func autoCacheDir() (string, error) {
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

func autoConfigPerms() (string, error) {
	cfg, err := config.Load()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(cfg.Path); os.IsNotExist(err) {
		if err := cfg.Save(); err != nil {
			return "", err
		}
		return "config.toml creado con valores por defecto (0600, sin secretos)", nil
	}
	return "", os.Chmod(cfg.Path, 0o600)
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorFix, "fix", false, "arregla automáticamente lo que no requiera sudo")
	doctorCmd.Flags().BoolVar(&doctorPackages, "packages", false, "imprime UNA línea que instala todos los paquetes del sistema necesarios en esta distribución")
}
