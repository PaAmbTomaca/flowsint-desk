package secrets

import (
	"strings"
	"testing"
)

// Usa valores de SONDA, nunca los reales del llavero/sistema.
func TestRedactEnmascaraValoresLargos(t *testing.T) {
	sec := map[string]string{
		"SONDA_SECRET": "valor-sonda-secreto-123",
		"SONDA_CORTA":  "abc",
	}
	in := "el token es valor-sonda-secreto-123 y la corta abc"
	out := Redact(in, sec)
	if strings.Contains(out, "valor-sonda-secreto-123") {
		t.Fatal("Redact no enmascaró el valor largo")
	}
	if !strings.Contains(out, "***") {
		t.Fatal("falta la máscara *** en la salida")
	}
	if !strings.Contains(out, "abc") {
		t.Fatal("no debe enmascarar valores de menos de 8 caracteres")
	}
}
