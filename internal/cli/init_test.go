package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("sin HOME")
	}
	t.Setenv("SONDA_DIR", "/datos")
	casos := map[string]string{
		"~/pCloudDrive/vault": filepath.Join(home, "pCloudDrive", "vault"),
		"~":                   home,
		"$SONDA_DIR/vault":    "/datos/vault",
		"  /abs/vault  ":      "/abs/vault",
		"/abs/~raro":          "/abs/~raro",
	}
	for in, want := range casos {
		got, err := expandPath(in)
		if err != nil || got != want {
			t.Errorf("expandPath(%q) = %q, %v; esperaba %q", in, got, err, want)
		}
	}
}
