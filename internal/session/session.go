// Package session gestiona el estado local de una sesión `up` viva.
// Fase 1: session.json dentro del directorio de ejecución (tmpfs).
// Fase 3 lo extiende con el bloqueo en el almacén (lock.json multi-host).
package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
)

type Session struct {
	PID     int    `json:"pid"`
	Port    int    `json:"port"`
	Started string `json:"started"` // RFC3339
	Host    string `json:"host"`
	BootID  string `json:"boot_id"`
}

func file(runDir string) string { return filepath.Join(runDir, "session.json") }

func Write(runDir string, s *Session) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file(runDir), b, 0o600)
}

func Read(runDir string) (*Session, error) {
	b, err := os.ReadFile(file(runDir))
	if err != nil {
		return nil, err
	}
	var s Session
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Alive comprueba si el proceso de la sesión sigue vivo (signal 0).
func (s *Session) Alive() bool {
	if s == nil || s.PID <= 0 {
		return false
	}
	return syscall.Kill(s.PID, 0) == nil
}

func Remove(runDir string) { _ = os.Remove(file(runDir)) }
