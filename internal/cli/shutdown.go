package cli

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/godbus/dbus/v5"
)

// watchShutdown pide a systemd-logind un inhibidor de apagado en modo "delay"
// y avisa (onShutdown) en cuanto el equipo empieza a apagarse o reiniciarse.
//
// Por qué así: lanzar `systemd-inhibit ... sleep infinity` (lo que se hacía
// antes) retrasa el apagado pero no gana tiempo útil: nadie se entera de que
// el apagado ha empezado, logind agota su espera y entonces llega SIGTERM a
// la vez a `up` y al cliente de pCloud, con lo que el snapshot final suele
// fallar. Escuchando PrepareForShutdown el snapshot empieza DENTRO de la
// ventana de espera, con pCloud aún montado.
//
// release suelta el inhibidor (hay que llamarlo al terminar el snapshot para
// que el apagado continúe). La ventana la fija InhibitDelayMaxSec de logind
// (5 s por defecto): `doctor` avisa si es corta.
func watchShutdown(onShutdown func()) (release func(), err error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, err
	}
	const (
		dest  = "org.freedesktop.login1"
		path  = dbus.ObjectPath("/org/freedesktop/login1")
		iface = "org.freedesktop.login1.Manager"
	)
	var fd dbus.UnixFD
	if err := conn.Object(dest, path).Call(iface+".Inhibit", 0,
		"shutdown", "flowsint-desk", "snapshot final cifrado antes de apagar", "delay").Store(&fd); err != nil {
		_ = conn.Close()
		return nil, err
	}
	if err := conn.AddMatchSignal(dbus.WithMatchObjectPath(path), dbus.WithMatchInterface(iface),
		dbus.WithMatchMember("PrepareForShutdown")); err != nil {
		_ = syscall.Close(int(fd))
		_ = conn.Close()
		return nil, err
	}
	ch := make(chan *dbus.Signal, 4)
	conn.Signal(ch)
	go func() {
		for sig := range ch {
			if sig.Name != iface+".PrepareForShutdown" || len(sig.Body) != 1 {
				continue
			}
			if empieza, ok := sig.Body[0].(bool); ok && empieza {
				onShutdown()
			}
		}
	}()
	soltado := false
	return func() {
		if soltado {
			return
		}
		soltado = true
		_ = syscall.Close(int(fd)) // cerrar el descriptor = soltar el inhibidor
		_ = conn.Close()
	}, nil
}

// legacyInhibit es la reserva cuando no hay D-Bus de sistema: el inhibidor
// "ciego" de antes (retrasa el apagado, sin aviso previo).
func legacyInhibit() (release func(), err error) {
	if _, err := exec.LookPath("systemd-inhibit"); err != nil {
		return nil, err
	}
	c := exec.Command("systemd-inhibit", "--what=shutdown", "--mode=delay",
		"--who=flowsint-desk", "--why=sesión flowsint-desk activa (snapshot final al apagar)",
		"sleep", "infinity")
	if err := c.Start(); err != nil {
		return nil, err
	}
	return func() { _ = c.Process.Kill(); _, _ = c.Process.Wait() }, nil
}

// inhibitDelaySeconds lee de logind cuánto espera como máximo a un inhibidor
// "delay" (InhibitDelayMaxUSec). Devuelve -1 si no se puede saber.
func inhibitDelaySeconds() int {
	out, err := exec.Command("busctl", "get-property", "org.freedesktop.login1", "/org/freedesktop/login1",
		"org.freedesktop.login1.Manager", "InhibitDelayMaxUSec").Output()
	if err != nil {
		return -1
	}
	return parseInhibitDelay(string(out))
}

// parseInhibitDelay interpreta la salida de busctl ("t 5000000" → 5).
func parseInhibitDelay(out string) int {
	f := strings.Fields(out)
	if len(f) != 2 || f[0] != "t" {
		return -1
	}
	us, err := strconv.ParseInt(f[1], 10, 64)
	if err != nil || us < 0 {
		return -1
	}
	return int(us / 1_000_000)
}

// minShutdownWindow: por debajo de esto no da tiempo ni a un snapshot pequeño.
const minShutdownWindow = 60

func shutdownWindowAdvice() string {
	return fmt.Sprintf("para que el snapshot final quepa al apagar, sube la espera de logind (una vez, con sudo):\n"+
		"       sudo mkdir -p /etc/systemd/logind.conf.d\n"+
		"       printf '[Login]\\nInhibitDelayMaxSec=%d\\n' | sudo tee /etc/systemd/logind.conf.d/flowsint-desk.conf\n"+
		"     y reinicia el equipo. Sin esto, al apagar cuenta el último snapshot automático (como mucho save_interval_min atrás)",
		2*minShutdownWindow)
}
