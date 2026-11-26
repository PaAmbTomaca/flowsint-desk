// Package passphrase gestiona la única llave del sistema: la frase de paso.
// Puede vivir en el llavero del sistema (go-keyring / Secret Service) o pedirse
// por terminal. NUNCA se imprime ni se pasa por argv.
package passphrase

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/zalando/go-keyring"
	"golang.org/x/term"
)

const (
	service = "flowsint-desk"
	account = "passphrase"
)

var ErrNotInKeyring = errors.New("frase no guardada en el llavero")

// EnvNoKeyring desactiva el llavero para este proceso (lo usa `selftest`,
// que trabaja con un almacén desechable y no debe leer ni escribir la frase
// real del usuario).
const EnvNoKeyring = "FLOWSINT_DESK_NO_KEYRING"

func keyringOff() bool { return os.Getenv(EnvNoKeyring) != "" }

// stdinReader compartido para no perder datos bufferizados entre prompts.
var stdinReader = bufio.NewReader(os.Stdin)

// KeyringAvailable sondea el Secret Service: true si el servicio responde
// (da igual que la clave de sonda no exista; eso significa que hay llavero).
func KeyringAvailable() bool {
	if keyringOff() {
		return false
	}
	_, err := keyring.Get(service, "doctor-probe")
	return errors.Is(err, keyring.ErrNotFound)
}

// FromKeyring devuelve la frase guardada en el llavero, o ErrNotInKeyring.
func FromKeyring() (string, error) {
	if keyringOff() {
		return "", ErrNotInKeyring
	}
	p, err := keyring.Get(service, account)
	if err != nil {
		return "", ErrNotInKeyring
	}
	return p, nil
}

// SaveKeyring guarda la frase en el llavero.
func SaveKeyring(p string) error {
	if keyringOff() {
		return errors.New("llavero desactivado para este proceso")
	}
	return keyring.Set(service, account, p)
}

// DeleteKeyring borra la frase del llavero.
func DeleteKeyring() error {
	err := keyring.Delete(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// Prompt pide la frase por terminal sin eco. minLen es la longitud mínima.
// Si stdin no es un TTY (p. ej. un pipe en pruebas), lee una línea de stdin.
func Prompt(label string, minLen int) (string, error) {
	fmt.Fprintf(os.Stderr, "%s: ", label)
	var p string
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", err
		}
		p = string(b)
	} else {
		line, err := stdinReader.ReadString('\n')
		if err != nil && line == "" {
			fmt.Fprintln(os.Stderr)
			return "", errors.New("no hay terminal ni frase por stdin: ejecuta el comando en una terminal (o guarda la frase en el llavero)")
		}
		p = strings.TrimRight(line, "\r\n")
	}
	if len(p) < minLen {
		return "", fmt.Errorf("la frase debe tener al menos %d caracteres", minLen)
	}
	return p, nil
}

// PromptTwice la pide dos veces y exige coincidencia (para init/passwd).
func PromptTwice(minLen int) (string, error) {
	p1, err := Prompt("Frase de paso", minLen)
	if err != nil {
		return "", err
	}
	p2, err := Prompt("Repite la frase de paso", minLen)
	if err != nil {
		return "", err
	}
	if p1 != p2 {
		return "", errors.New("las frases no coinciden")
	}
	if len(p1) < RecommendedLen {
		fmt.Fprintf(os.Stderr, "aviso: frase de %d caracteres. Es la ÚNICA llave de todo el almacén (y pCloud guarda el repo): se recomiendan %d o más, p. ej. 5-6 palabras al azar. Puedes cambiarla luego con `flowsint-desk passwd`.\n", len(p1), RecommendedLen)
	}
	return p1, nil
}

// RecommendedLen es la longitud a partir de la cual no se avisa de frase
// corta. El mínimo duro (MinLen) no se sube para no romper flujos existentes.
const (
	MinLen         = 8
	RecommendedLen = 16
)

// ReadLine lee una línea de stdin por el lector compartido (para que los
// prompts de texto y los de la frase no se roben datos entre sí cuando stdin
// es un pipe).
func ReadLine() string {
	line, _ := stdinReader.ReadString('\n')
	return strings.TrimSpace(line)
}

// Get devuelve la frase: primero del llavero; si no está, la pide por terminal.
func Get() (string, bool, error) {
	if p, err := FromKeyring(); err == nil {
		return p, true, nil
	}
	p, err := Prompt("Frase de paso", MinLen)
	if err != nil {
		return "", false, err
	}
	return p, false, nil
}

// AskSaveKeyring pregunta si recordar la frase en el llavero.
func AskSaveKeyring() (bool, error) {
	if keyringOff() {
		return false, nil
	}
	fmt.Fprintf(os.Stderr, "¿Guardar la frase en el llavero del sistema (Secret Service)? [s/N]: ")
	ans := ReadLine()
	return ans == "s" || ans == "S" || ans == "sí" || ans == "si" || ans == "y" || ans == "Y", nil
}
