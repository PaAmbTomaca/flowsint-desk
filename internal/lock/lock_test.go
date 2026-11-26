package lock

import (
	"os"
	"strings"
	"testing"
	"time"
)

func conMaquina(t *testing.T, id string) {
	t.Helper()
	old := machineID
	machineID = func() string { return id }
	t.Cleanup(func() { machineID = old })
}

func TestCicloBasico(t *testing.T) {
	conMaquina(t, "aaaa")
	v := t.TempDir()
	if err := Acquire(v, "torre", 100, "boot"); err != nil {
		t.Fatal(err)
	}
	if !Mine(v, "torre", 100) || Mine(v, "torre", 101) {
		t.Fatal("Mine debe distinguir por pid")
	}
	if err := Touch(v, "torre", 100); err != nil {
		t.Fatal(err)
	}
	if err := Touch(v, "torre", 101); err == nil {
		t.Fatal("Touch con otro pid debe fallar")
	}
	if err := Release(v, "torre", 101); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Path(v)); err != nil {
		t.Fatal("Release con otro pid no debe borrar el bloqueo")
	}
	if err := Release(v, "torre", 100); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Path(v)); !os.IsNotExist(err) {
		t.Fatal("tras Release el bloqueo debe desaparecer")
	}
	if _, err := os.Stat(Path(v) + ".tmp"); !os.IsNotExist(err) {
		t.Fatal("no debe quedar el temporal de la escritura atómica")
	}
}

func TestOtroHostRecienteYCaducado(t *testing.T) {
	conMaquina(t, "aaaa")
	v := t.TempDir()
	if err := Acquire(v, "torre", 100, "boot"); err != nil {
		t.Fatal(err)
	}
	conMaquina(t, "bbbb")
	err := Acquire(v, "portatil", 200, "boot2")
	if err == nil || !strings.Contains(err.Error(), "bloqueado por otro equipo") {
		t.Fatalf("latido reciente de otro host: %v", err)
	}
	// caducar el latido
	l, _ := Read(v)
	l.Heartbeat = time.Now().Add(-(StaleMinutes + 1) * time.Minute).UTC().Format(time.RFC3339)
	if err := write(v, l); err != nil {
		t.Fatal(err)
	}
	err = Acquire(v, "portatil", 200, "boot2")
	if err == nil || !strings.Contains(err.Error(), "CADUCADO") {
		t.Fatalf("latido caducado de otro host: %v", err)
	}
}

func TestMismoHostnameDistintaMaquina(t *testing.T) {
	conMaquina(t, "aaaa")
	v := t.TempDir()
	if err := Acquire(v, "cachyos", 100, "boot"); err != nil {
		t.Fatal(err)
	}
	conMaquina(t, "bbbb")
	if err := Acquire(v, "cachyos", 200, "boot2"); err == nil {
		t.Fatal("dos equipos con el mismo hostname NO deben pisarse el bloqueo")
	}
	if Mine(v, "cachyos", 100) {
		t.Fatal("mismo hostname y pid en otra máquina no es nuestro bloqueo")
	}
}

func TestBloqueoAntiguoSinMaquinaSigueSiendoLocal(t *testing.T) {
	conMaquina(t, "aaaa")
	v := t.TempDir()
	// bloqueo escrito por una versión anterior (sin campo equipo)
	if err := write(v, &Lock{Host: "torre", PID: 7, Heartbeat: time.Now().UTC().Format(time.RFC3339)}); err != nil {
		t.Fatal(err)
	}
	if err := Acquire(v, "torre", 100, "boot"); err != nil {
		t.Fatalf("un bloqueo propio de una versión anterior (kill -9) debe poder retomarse: %v", err)
	}
}
