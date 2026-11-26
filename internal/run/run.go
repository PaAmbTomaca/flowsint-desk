// Package run gestiona el directorio de ejecución efímero en tmpfs.
// Todos los datos en claro (secretos, volcados, sockets, data-root de Docker)
// viven SOLO aquí y se borran al cerrar la sesión.
package run

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

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
	// /tmp es de todos y el nombre es predecible (el boot_id es público): no
	// basta con MkdirAll. Se crea 0700 y se exige que sea un directorio real,
	// nuestro y privado antes de escribir ni un secreto dentro.
	if err := os.Mkdir(dir, 0o700); err != nil && !os.IsExist(err) {
		return "", err
	}
	if err := checkPrivate(dir); err != nil {
		return "", err
	}
	if ok, err := IsTmpfs(dir); err != nil {
		return "", err
	} else if !ok {
		return "", fmt.Errorf("REFUSO escribir datos en claro: %s no está en tmpfs (statfs)", dir)
	}
	for _, sub := range []string{"rk-state", "dockerd-data", "dockerd-exec", "stack", "neo4j-import", "staging", "restore"} {
		p := filepath.Join(dir, sub)
		if err := os.MkdirAll(p, 0o700); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// Private es checkPrivate para otros paquetes (restos .stale en /tmp).
func Private(dir string) error { return checkPrivate(dir) }

// checkPrivate exige que dir sea un directorio de verdad (no un enlace
// simbólico), propiedad del usuario efectivo y sin permisos para grupo/otros.
// Defiende de que otro usuario local pre-cree /tmp/flowsint-desk-<boot_id>
// (o un enlace a disco) para leer la frase, los secretos o los volcados.
func checkPrivate(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return fmt.Errorf("REFUSO usar %s: no es un directorio real (¿enlace simbólico?); bórralo y reintenta", dir)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("REFUSO usar %s: no se pudo comprobar su propietario", dir)
	}
	if int(st.Uid) != os.Geteuid() {
		return fmt.Errorf("REFUSO usar %s: pertenece al uid %d, no a ti (uid %d); otro usuario lo ha creado antes", dir, st.Uid, os.Geteuid())
	}
	if fi.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("REFUSO usar %s: permisos %04o y no se pudieron cerrar a 0700: %w", dir, fi.Mode().Perm(), err)
		}
	}
	return nil
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
