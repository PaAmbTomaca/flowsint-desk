package cli

import (
	"reflect"
	"testing"

	"flowsint-desk/internal/dock"
)

func TestDistroFamily(t *testing.T) {
	casos := map[string]string{
		"NAME=\"CachyOS Linux\"\nID=cachyos\nID_LIKE=arch\n": famArch,
		"ID=arch\n": famArch,
		"PRETTY_NAME=\"Debian GNU/Linux 13 (trixie)\"\nID=debian\n": famDebian,
		"ID=ubuntu\nID_LIKE=debian\n":                               famDebian,
		"ID=pop\nID_LIKE=\"ubuntu debian\"\n":                       famDebian,
		"ID=fedora\n":                                               famFedora,
		"ID=rocky\nID_LIKE=\"rhel centos fedora\"\n":                famFedora,
		"ID=opensuse-leap\n":                                        famOther,
		"":                                                          famOther,
	}
	for in, want := range casos {
		if got := distroFamily(in); got != want {
			t.Errorf("distroFamily(%q) = %s, esperaba %s", in, got, want)
		}
	}
}

func TestInstallHint(t *testing.T) {
	if got := installHint(famDebian, "newuidmap"); got != "sudo apt install uidmap" {
		t.Errorf("debian newuidmap: %q", got)
	}
	if got := installHint(famArch, "newuidmap"); got != "sudo pacman -S shadow" {
		t.Errorf("arch newuidmap: %q", got)
	}
	if got := installHint(famFedora, "newuidmap"); got != "sudo dnf install shadow-utils" {
		t.Errorf("fedora newuidmap: %q", got)
	}
	if got := allPackagesCmd(famDebian, dock.EnginePodman); got != "sudo apt install podman aardvark-dns passt uidmap restic" {
		t.Errorf("allPackagesCmd debian/podman: %q", got)
	}
	if got := allPackagesCmd(famDebian, dock.EngineDocker); got != "sudo apt install docker.io docker-compose uidmap rootlesskit slirp4netns fuse-overlayfs restic" {
		t.Errorf("allPackagesCmd debian: %q", got)
	}
	if got := allPackagesCmd(famOther, dock.EngineDocker); got != "" {
		t.Errorf("familia desconocida no debe inventar comando: %q", got)
	}
	if got := installHint(famOther, "restic"); got == "" || got[:4] == "sudo" {
		t.Errorf("distro desconocida no debe inventar un comando: %q", got)
	}
}

func TestPCloudMounts(t *testing.T) {
	mounts := `tmpfs /tmp tmpfs rw,nosuid 0 0
/dev/nvme0n1p2 / btrfs rw 0 0
pCloud.fs /home/pollo/pCloudDrive fuse.pCloud.fs rw,nosuid,nodev,relatime,user_id=1000 0 0
portal /run/user/1000/doc fuse.portal rw 0 0
pcloud /home/pollo/Mi\040Nube fuse rw 0 0
`
	want := []string{"/home/pollo/pCloudDrive", "/home/pollo/Mi Nube"}
	if got := pcloudMounts(mounts); !reflect.DeepEqual(got, want) {
		t.Fatalf("pcloudMounts = %v, esperaba %v", got, want)
	}
	if got := pcloudMounts("tmpfs /tmp tmpfs rw 0 0\n"); len(got) != 0 {
		t.Fatalf("sin pCloud → %v", got)
	}
}
