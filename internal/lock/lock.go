// Package lock implementa el bloqueo entre equipos: un fichero
// flowsint-desk.lock.json en la carpeta del almacén (pCloud) con host, pid,
// boot_id, iniciado y latido. pCloud no es atómico: es una protección pensada
// para una persona con dos equipos, no para concurrencia real.
package lock

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Lock struct {
	Host string `json:"host"`
	// Machine identifica la instalación (hash de /etc/machine-id): dos equipos
	// pueden llamarse igual (p. ej. el hostname por defecto de la distro) y
	// entonces el host solo no distingue torre de portátil. Vacío en bloqueos
	// escritos por versiones anteriores.
	Machine   string `json:"equipo,omitempty"`
	PID       int    `json:"pid"`
	BootID    string `json:"boot_id"`
	Started   string `json:"iniciado"` // RFC3339
	Heartbeat string `json:"latido"`   // RFC3339
}

func Path(vault string) string { return filepath.Join(vault, "flowsint-desk.lock.json") }

func Read(vault string) (*Lock, error) {
	b, err := os.ReadFile(Path(vault))
	if err != nil {
		return nil, err
	}
	var l Lock
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, err
	}
	return &l, nil
}

// StaleMinutes: un bloqueo de otro host con latido más reciente que esto se
// considera vivo (fase 3: 15 min).
const StaleMinutes = 15

// machineID devuelve un identificador estable de ESTA instalación: sha256 de
// /etc/machine-id truncado (el fichero de bloqueo lo ve pCloud: no se publica
// el machine-id en crudo). "" si no se puede leer. Variable para los tests.
var machineID = func() string {
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if b, err := os.ReadFile(p); err == nil {
			if id := strings.TrimSpace(string(b)); id != "" {
				h := sha256.Sum256([]byte("flowsint-desk:" + id))
				return hex.EncodeToString(h[:8])
			}
		}
	}
	return ""
}

// IsLocal dice si el bloqueo l pertenece a ESTE equipo: mismo host y, cuando
// ambos lados conocen su identificador de máquina, también el mismo.
func IsLocal(l *Lock, host string) bool {
	if l == nil || l.Host != host {
		return false
	}
	mine := machineID()
	return l.Machine == "" || mine == "" || l.Machine == mine
}

// write guarda el bloqueo de forma atómica (temporal + rename): un fichero a
// medio escribir se leería como "sin bloqueo" desde el otro equipo. Si el
// rename falla (FUSE raro), cae a la escritura directa.
func write(vault string, l *Lock) error {
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	tmp := Path(vault) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err == nil {
		if err := os.Rename(tmp, Path(vault)); err == nil {
			return nil
		}
		_ = os.Remove(tmp)
	}
	return os.WriteFile(Path(vault), b, 0o600)
}

// Acquire toma el bloqueo. Falla si hay un bloqueo de OTRO equipo, sea con
// latido reciente (espera) o caducado (sugiere unlock --force).
func Acquire(vault, host string, pid int, bootID string) error {
	if old, err := Read(vault); err == nil && !IsLocal(old, host) {
		quien := "host=" + old.Host
		if old.Host == host {
			quien += " (OTRO equipo con el mismo nombre de host)"
		}
		hb, _ := time.Parse(time.RFC3339, old.Heartbeat)
		edad := time.Since(hb).Round(time.Second)
		if time.Since(hb) < StaleMinutes*time.Minute {
			return fmt.Errorf("almacén bloqueado por otro equipo: %s pid=%d latido hace %s — espera o `flowsint-desk unlock --force`",
				quien, old.PID, edad)
		}
		return fmt.Errorf("hay un bloqueo CADUCADO de otro equipo (%s, latido hace %s) — usa `flowsint-desk unlock --force`", quien, edad)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	return write(vault, &Lock{
		Host: host, Machine: machineID(), PID: pid, BootID: bootID,
		Started: now, Heartbeat: now,
	})
}

// Touch renueva el latido (solo si el bloqueo es nuestro).
func Touch(vault, host string, pid int) error {
	l, err := Read(vault)
	if err != nil {
		return err
	}
	if !IsLocal(l, host) || l.PID != pid {
		return fmt.Errorf("el bloqueo ya no es nuestro (ahora: host=%s pid=%d)", l.Host, l.PID)
	}
	l.Heartbeat = time.Now().UTC().Format(time.RFC3339)
	return write(vault, l)
}

// Mine comprueba si el bloqueo actual es nuestro.
func Mine(vault, host string, pid int) bool {
	l, err := Read(vault)
	if err != nil {
		return false
	}
	return IsLocal(l, host) && l.PID == pid
}

// Release suelta el bloqueo (solo si es nuestro).
func Release(vault, host string, pid int) error {
	if !Mine(vault, host, pid) {
		return nil
	}
	return os.Remove(Path(vault))
}

// ForceRemove borra el bloqueo sin comprobar (unlock --force).
func ForceRemove(vault string) error { return os.Remove(Path(vault)) }
