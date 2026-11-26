package run

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckPrivateAceptaDirPropioYCierraPermisos(t *testing.T) {
	d := filepath.Join(t.TempDir(), "run")
	if err := os.Mkdir(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := checkPrivate(d); err != nil {
		t.Fatalf("dir propio: %v", err)
	}
	fi, _ := os.Stat(d)
	if fi.Mode().Perm() != 0o700 {
		t.Fatalf("permisos tras checkPrivate = %04o, esperaba 0700", fi.Mode().Perm())
	}
}

func TestCheckPrivateRechazaSymlinkYFichero(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := checkPrivate(link); err == nil {
		t.Fatal("un enlace simbólico debe rechazarse aunque apunte a un dir propio")
	}
	f := filepath.Join(base, "fichero")
	if err := os.WriteFile(f, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkPrivate(f); err == nil {
		t.Fatal("un fichero regular debe rechazarse")
	}
}

func TestCleanupRechazaRutasPeligrosas(t *testing.T) {
	for _, p := range []string{"", "/", "/tmp"} {
		if err := Cleanup(p); err == nil {
			t.Fatalf("Cleanup(%q) debería negarse", p)
		}
	}
}
