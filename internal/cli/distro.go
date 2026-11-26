package cli

import (
	"os"
	"path/filepath"
	"strings"

	"flowsint-desk/internal/dock"
)

// Familias de distribución que la CLI sabe guiar.
const (
	famArch   = "arch"
	famDebian = "debian"
	famFedora = "fedora"
	famOther  = "otra"
)

// distroFamily clasifica la distribución a partir del contenido de
// /etc/os-release (ID e ID_LIKE): CachyOS/Manjaro → arch; Debian/Ubuntu/Mint
// → debian; Fedora/RHEL y derivadas → fedora.
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
		case "fedora", "rhel", "centos", "rocky", "almalinux":
			return famFedora
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
	"rootlesskit":    {famArch: "rootlesskit", famDebian: "rootlesskit", famFedora: "rootlesskit"},
	"slirp4netns":    {famArch: "slirp4netns", famDebian: "slirp4netns", famFedora: "slirp4netns"},
	"fuse-overlayfs": {famArch: "fuse-overlayfs", famDebian: "fuse-overlayfs", famFedora: "fuse-overlayfs"},
	"newuidmap":      {famArch: "shadow", famDebian: "uidmap", famFedora: "shadow-utils"},
	"docker":         {famArch: "docker", famDebian: "docker.io", famFedora: "moby-engine"},
	"compose":        {famArch: "docker-compose", famDebian: "docker-compose", famFedora: "docker-compose"},
	"restic":         {famArch: "restic", famDebian: "restic", famFedora: "restic"},
	// motor Podman (nunca «podman-docker»: suplanta /usr/bin/docker y entra en
	// conflicto con el Docker del sistema)
	"podman":   {famArch: "podman", famDebian: "podman", famFedora: "podman"},
	"aardvark": {famArch: "aardvark-dns", famDebian: "aardvark-dns", famFedora: "aardvark-dns"},
	"pasta":    {famArch: "passt", famDebian: "passt", famFedora: "passt"},
}

// installVerb: orden del gestor de paquetes de cada familia.
var installVerb = map[string]string{
	famArch:   "sudo pacman -S",
	famDebian: "sudo apt install",
	famFedora: "sudo dnf install",
}

// enginePkgReqs: requisitos de paquetes del sistema de cada motor.
var enginePkgReqs = map[string][]string{
	dock.EngineDocker: {"docker", "compose", "newuidmap", "rootlesskit", "slirp4netns", "fuse-overlayfs", "restic"},
	dock.EnginePodman: {"podman", "aardvark", "pasta", "newuidmap", "restic"},
}

// allPackagesCmd devuelve UNA línea que instala todos los paquetes del sistema
// que necesita flowsint-desk con ese motor en esa familia ("" si no se conoce).
func allPackagesCmd(fam, engine string) string {
	verb := installVerb[fam]
	if verb == "" {
		return ""
	}
	seen := map[string]bool{}
	var list []string
	for _, req := range enginePkgReqs[engine] {
		if p := pkgs[req][fam]; p != "" && !seen[p] {
			seen[p] = true
			list = append(list, p)
		}
	}
	return verb + " " + strings.Join(list, " ")
}

// installHint devuelve el comando de instalación del paquete del sistema para
// un requisito ("sudo pacman -S x" / "sudo apt install x"). La CLI nunca lo
// ejecuta: solo lo muestra.
func installHint(fam, req string) string {
	p := pkgs[req][fam]
	if p == "" || installVerb[fam] == "" {
		return "instala «" + req + "» con el gestor de paquetes de tu distribución"
	}
	return installVerb[fam] + " " + p
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
