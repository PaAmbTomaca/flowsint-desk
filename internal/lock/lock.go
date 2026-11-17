// Package lock implementa el bloqueo entre equipos: un fichero
// flowsint-desk.lock.json en la carpeta del almacén (pCloud) con host, pid,
// boot_id, iniciado y latido. pCloud no es atómico: es una protección pensada
// para una persona con dos equipos, no para concurrencia real.
package lock

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Lock struct {
	Host      string `json:"host"`
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

// Acquire toma el bloqueo. Falla si hay un bloqueo de OTRO host con latido
// reciente (sugiere unlock --force si está caducado).
func Acquire(vault, host string, pid int, bootID string) error {
	if old, err := Read(vault); err == nil {
		if old.Host != host {
			hb, _ := time.Parse(time.RFC3339, old.Heartbeat)
			if time.Since(hb) < StaleMinutes*time.Minute {
				return fmt.Errorf("almacén bloqueado por otro equipo: host=%s pid=%d latido hace %s — espera o `flowsint-desk unlock --force`",
					old.Host, old.PID, time.Since(hb).Round(time.Second))
			}
			return fmt.Errorf("hay un bloqueo CADUCADO de otro host (%s, latido hace %s) — usa `flowsint-desk unlock --force`", old.Host, time.Since(hb).Round(time.Second))
		}
	}
	l := &Lock{
		Host: host, PID: pid, BootID: bootID,
		Started: time.Now().UTC().Format(time.RFC3339),
		Heartbeat: time.Now().UTC().Format(time.RFC3339),
	}
	b, _ := json.MarshalIndent(l, "", "  ")
	return os.WriteFile(Path(vault), b, 0o600)
}

// Touch renueva el latido (solo si el bloqueo es nuestro).
func Touch(vault, host string, pid int) error {
	l, err := Read(vault)
	if err != nil {
		return err
	}
	if l.Host != host || l.PID != pid {
		return fmt.Errorf("el bloqueo ya no es nuestro (ahora: host=%s pid=%d)", l.Host, l.PID)
	}
	l.Heartbeat = time.Now().UTC().Format(time.RFC3339)
	b, _ := json.MarshalIndent(l, "", "  ")
	return os.WriteFile(Path(vault), b, 0o600)
}

// Mine comprueba si el bloqueo actual es nuestro.
func Mine(vault, host string, pid int) bool {
	l, err := Read(vault)
	if err != nil {
		return false
	}
	return l.Host == host && l.PID == pid
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
