package cli

import (
	"os"
	"path/filepath"
	"strings"
)

// Familias de distribución que la CLI sabe guiar.
const (
	famArch   = "arch"
	famDebian = "debian"
	famOther  = "otra"
)

// distroFamily clasifica la distribución a partir del contenido de
// /etc/os-release (ID e ID_LIKE): CachyOS/Manjaro → arch; Debian/Ubuntu/Mint
// → debian.
func distroFamily(osRelease string) string {
	var ids []string
	for _, line := range strings.Split(osRelease, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || (k != "ID" && k != "ID_LIKE") {
			continue
		}
		ids = append(ids, strings.Fields(strings.ToLower(strings.Trim(v, `"'`)))...)
	}
	for _, id := range ids {
		switch id {
		case "arch", "cachyos", "manjaro", "endeavouros":
			return famArch
		case "debian", "ubuntu", "linuxmint", "raspbian":
			return famDebian
		}
	}
	return famOther
}

func currentFamily() string {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return famOther
	}
	return distroFamily(string(b))
}

// pkgs: nombre del paquete que trae cada requisito, por familia.
var pkgs = map[string]map[string]string{
	"rootlesskit":    {famArch: "rootlesskit", famDebian: "rootlesskit"},
	"slirp4netns":    {famArch: "slirp4netns", famDebian: "slirp4netns"},
	"fuse-overlayfs": {famArch: "fuse-overlayfs", famDebian: "fuse-overlayfs"},
	"newuidmap":      {famArch: "shadow", famDebian: "uidmap"},
	"docker":         {famArch: "docker", famDebian: "docker.io"},
	"compose":        {famArch: "docker-compose", famDebian: "docker-compose"},
	"restic":         {famArch: "restic", famDebian: "restic"},
}

// installHint devuelve el comando de instalación del paquete del sistema para
// un requisito ("sudo pacman -S x" / "sudo apt install x"). La CLI nunca lo
// ejecuta: solo lo muestra.
func installHint(fam, req string) string {
	p := pkgs[req][fam]
	switch {
	case p == "":
		return "instala «" + req + "» con el gestor de paquetes de tu distribución"
	case fam == famArch:
		return "sudo pacman -S " + p
	default:
		return "sudo apt install " + p
	}
}

// unescapeMount deshace los escapes octales de /proc/mounts (\040 = espacio).
func unescapeMount(s string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\134`, `\`).Replace(s)
}

// pcloudMounts devuelve los puntos de montaje de pCloud Drive que aparecen en
// el contenido de /proc/mounts (montajes FUSE cuyo dispositivo o tipo
// menciona pCloud).
func pcloudMounts(procMounts string) []string {
	var out []string
	for _, line := range strings.Split(procMounts, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || !strings.Contains(strings.ToLower(f[2]), "fuse") {
			continue
		}
		if strings.Contains(strings.ToLower(f[0]+" "+f[2]), "pcloud") {
			out = append(out, unescapeMount(f[1]))
		}
	}
	return out
}

// detectPCloud devuelve el punto de montaje de pCloud Drive, o "" si no hay
// ninguno montado ahora mismo.
func detectPCloud() string {
	b, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return ""
	}
	if m := pcloudMounts(string(b)); len(m) > 0 {
		return m[0]
	}
	return ""
}

// suggestedVault propone la ruta del almacén: dentro del pCloud detectado; si
// no hay, la de la configuración.
func suggestedVault(cfgVault string) string {
	if m := detectPCloud(); m != "" && !strings.HasPrefix(cfgVault, m+string(filepath.Separator)) {
		return filepath.Join(m, "flowsint-vault")
	}
	return cfgVault
}
