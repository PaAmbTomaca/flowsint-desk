// Package run gestiona el directorio de ejecución efímero en tmpfs.
// Todos los datos en claro (secretos, volcados, sockets, data-root de Docker)
// viven SOLO aquí y se borran al cerrar la sesión.
package run

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

const tmpfsMagic = 0x01021994 // TMPFS_MAGIC

// BootID lee /proc/sys/kernel/random/boot_id (identifica el arranque actual).
func BootID() (string, error) {
	b, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// Dir devuelve /tmp/flowsint-desk-<boot_id>.
func Dir() (string, error) {
	boot, err := BootID()
	if err != nil {
		return "", fmt.Errorf("leyendo boot_id: %w", err)
	}
	return filepath.Join("/tmp", "flowsint-desk-"+boot), nil
}

// IsTmpfs comprueba con statfs que el path está en tmpfs (TMPFS_MAGIC).
func IsTmpfs(path string) (bool, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return false, err
	}
	return uint64(st.Type) == tmpfsMagic, nil
}

// Ensure crea el directorio de ejecución (0700) y sus subdirectorios,
// exigiendo que /tmp sea tmpfs. Devuelve la ruta.
func Ensure() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	ok, err := IsTmpfs("/tmp")
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("REFUSO escribir datos en claro: /tmp no es tmpfs (statfs)")
	}
	for _, sub := range []string{"", "rk-state", "dockerd-data", "dockerd-exec", "stack", "neo4j-import", "staging", "restore"} {
		p := filepath.Join(dir, sub)
		if err := os.MkdirAll(p, 0o700); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// Socket es el socket del Docker rootless de esta sesión.
func Socket(dir string) string { return filepath.Join(dir, "docker.sock") }

// Cleanup borra el directorio de ejecución completo (datos en claro incluidos).
func Cleanup(dir string) error {
	if dir == "" || dir == "/" || dir == "/tmp" {
		return fmt.Errorf("ruta de limpieza inválida: %q", dir)
	}
	return os.RemoveAll(dir)
}
