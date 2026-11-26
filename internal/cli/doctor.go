package cli

import (
	"bytes"
	"compress/bzip2"
	"crypto/sha256"
	"encoding/hex"
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

var doctorFix bool

// doctorCheck es una comprobación del entorno. Si falla y warn=true solo
// avisa (no suma a los errores). fix es el paso exacto para arreglarlo a
// mano; auto, si no es nil, lo intenta solo con doctor --fix (nunca sudo).
type doctorCheck struct {
	name string
	warn bool
	run  func() (bool, string) // ok, detalle (se imprime entre paréntesis)
	fix  string
	auto func() (string, error) // devuelve una nota opcional
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
		_, err := runDoctor(doctorFix)
		return err
	},
}

// runDoctor ejecuta todos los chequeos; con fix=true intenta arreglar
// automáticamente los que no requieran sudo. Devuelve los que siguen en rojo.
func runDoctor(fix bool) ([]*doctorCheck, error) {
	cfg, _ := config.Load()
	u, _ := user.Current()
	fail := 0
	var pendientes []*doctorCheck
	for _, c := range doctorChecks(cfg, u.Username) {
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
	binFix := "`flowsint-desk doctor --fix` lo descarga a ~/.local/bin; a mano: ver la sección Instalación del README"

	return []*doctorCheck{
		// --- binarios rootless ---
		{name: "binario rootlesskit", run: look("rootlesskit"), fix: binFix, auto: autoArchBin("rootlesskit")},
		{name: "binario slirp4netns", run: look("slirp4netns"), fix: binFix, auto: autoArchBin("slirp4netns")},
		{name: "binario fuse-overlayfs", run: look("fuse-overlayfs"), fix: binFix, auto: autoArchBin("fuse-overlayfs")},
		{name: "binario dockerd-rootless.sh", run: look("dockerd-rootless.sh"), fix: binFix, auto: autoDockerdScript},
		{name: "binario newuidmap (paquete shadow)", run: look("newuidmap"),
			fix: "instala el paquete shadow del sistema (p. ej. sudo pacman -S shadow)"},
		{name: "binario newgidmap (paquete shadow)", run: look("newgidmap"),
			fix: "instala el paquete shadow del sistema (p. ej. sudo pacman -S shadow)"},

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
			fix:  "`flowsint-desk doctor --fix` descarga el binario oficial a ~/.local/bin; a mano: github.com/restic/restic/releases",
			auto: autoRestic},
		{name: "binario docker", run: look("docker"),
			fix: "instala el paquete docker (solo se usa el cliente y compose; el daemon del sistema NO se toca)"},
		{name: "docker compose v2",
			run: func() (bool, string) {
				out, err := exec.Command("docker", "compose", "version").CombinedOutput()
				if err != nil {
					return false, ""
				}
				return true, strings.TrimSpace(string(out))
			},
			fix: "instala el plugin docker-compose-plugin (p. ej. sudo pacman -S docker-compose)"},

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
		{name: "almacén existe y es escribible: " + vault,
			run: func() (bool, string) {
				if _, err := os.Stat(vault); err != nil {
					return false, "no existe"
				}
				return unix.Access(vault, unix.W_OK) == nil, ""
			},
			fix: "monta pCloud (p. ej. ~/pCloudDrive) y ejecuta `flowsint-desk init --vault " + vault + "` para crearlo"},
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
		{name: "binario systemd-inhibit (snapshot final al apagar el equipo)", warn: true,
			run: look("systemd-inhibit"),
			fix: "instala systemd (ya viene en la mayoría de distribuciones); sin él, al apagar vale el último snapshot automático"},
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
		{name: "conectividad HTTPS a ghcr.io:443 y github.com:443",
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

// paquetes Arch del README (extra/os/x86_64) que contienen cada binario.
var archBinPkgs = map[string]string{
	"rootlesskit":    "rootlesskit-3.3.0-1-x86_64.pkg.tar.zst",
	"slirp4netns":    "slirp4netns-1.3.7-1-x86_64.pkg.tar.zst",
	"fuse-overlayfs": "fuse-overlayfs-1.18-1-x86_64.pkg.tar.zst",
}

func autoArchBin(bin string) func() (string, error) {
	return func() (string, error) {
		pkg, ok := archBinPkgs[bin]
		if !ok {
			return "", fmt.Errorf("no hay paquete automático para %s", bin)
		}
		data, err := httpGet("https://geo.mirror.pkgbuild.com/extra/os/x86_64/" + pkg)
		if err != nil {
			return "", err
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
		if out, err := exec.Command("tar", "--zstd", "-xf", pf, "-C", tmp).CombinedOutput(); err != nil {
			return "", fmt.Errorf("tar: %v (%s)", err, strings.TrimSpace(string(out)))
		}
		src := filepath.Join(tmp, "usr", "bin", bin)
		b, err := os.ReadFile(src)
		if err != nil {
			return "", fmt.Errorf("el paquete no trae usr/bin/%s", bin)
		}
		if err := writeLocalBin(bin, b); err != nil {
			return "", err
		}
		return pkg, nil
	}
}

func autoDockerdScript() (string, error) {
	data, err := httpGet("https://raw.githubusercontent.com/moby/moby/master/contrib/dockerd-rootless.sh")
	if err != nil {
		return "", err
	}
	return "dockerd-rootless.sh (moby master)", writeLocalBin("dockerd-rootless.sh", data)
}

func autoRestic() (string, error) {
	const ver = "0.19.1"
	base := "https://github.com/restic/restic/releases/download/v" + ver + "/restic_" + ver + "_linux_amd64.bz2"
	bz, err := httpGet(base)
	if err != nil {
		return "", err
	}
	sum, err := httpGet(base + ".sha256")
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(sum))
	if len(fields) == 0 {
		return "", fmt.Errorf("sha256 oficial vacío")
	}
	h := sha256.Sum256(bz)
	got := hex.EncodeToString(h[:])
	if got != fields[0] {
		return "", fmt.Errorf("sha256 no cuadra (descargado %s, oficial %s)", got, fields[0])
	}
	raw, err := io.ReadAll(bzip2.NewReader(bytes.NewReader(bz)))
	if err != nil {
		return "", fmt.Errorf("bzip2: %w", err)
	}
	if err := writeLocalBin("restic", raw); err != nil {
		return "", err
	}
	return "restic v" + ver + " (sha256 verificado contra el .sha256 oficial)", nil
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
}
