package store

import (
	"errors"
	"os/exec"
	"testing"
)

func TestParseBackupIDConProgresoYRuido(t *testing.T) {
	out := `{"message_type":"status","percent_done":0.3,"total_files":4}
{"message_type":"status","percent_done":1,"total_files":4}
{"message_type":"summary","files_new":4,"snapshot_id":"3f2a9c0011223344"}
`
	if got := parseBackupID(out); got != "3f2a9c0011223344" {
		t.Fatalf("snapshot_id = %q", got)
	}
}

func TestParseBackupIDSinResumen(t *testing.T) {
	for _, out := range []string{"", "ruido\n", `{"message_type":"status"}` + "\n", `{"message_type":"summary"}`} {
		if got := parseBackupID(out); got != "" {
			t.Fatalf("esperaba vacío para %q, obtuve %q", out, got)
		}
	}
}

func TestClassifyPorCodigoDeSalida(t *testing.T) {
	exit := func(code string) error {
		return exec.Command("sh", "-c", "exit "+code).Run()
	}
	if got := classify(exit("12"), ""); !errors.Is(got, ErrWrongPassword) {
		t.Fatalf("exit 12 → %v", got)
	}
	if got := classify(exit("10"), ""); !errors.Is(got, ErrNoRepo) {
		t.Fatalf("exit 10 → %v", got)
	}
	if got := classify(exit("1"), "otro fallo"); got != nil {
		t.Fatalf("exit 1 genérico → %v", got)
	}
}

func TestClassifyPorTexto(t *testing.T) {
	err := exec.Command("sh", "-c", "exit 1").Run()
	if got := classify(err, "Fatal: wrong password or no key found"); !errors.Is(got, ErrWrongPassword) {
		t.Fatalf("texto wrong password → %v", got)
	}
	if got := classify(err, "Fatal: repository does not exist: unable to open config file\nIs there a repository at the following location?"); !errors.Is(got, ErrNoRepo) {
		t.Fatalf("texto sin repo → %v", got)
	}
}
