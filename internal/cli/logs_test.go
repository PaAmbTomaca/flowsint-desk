package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Valores de SONDA, nunca reales.
func TestRedactValuesIncluyeFraseDeResticPass(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "secrets.env"), []byte("AUTH_SECRET=sonda-auth-secret-123\nPOSTGRES_USER=flowsint\nMASTER_VAULT_KEY_V1=base64:c29uZGEtY2xhdmU=\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// sin fichero `passphrase`: la frase vino del llavero/prompt y solo está en restic-pass
	if err := os.WriteFile(filepath.Join(d, "restic-pass"), []byte("frase de sonda larga"), 0o600); err != nil {
		t.Fatal(err)
	}
	out := redactText("pass=frase de sonda larga auth=sonda-auth-secret-123 user=flowsint key=c29uZGEtY2xhdmU=", redactValues(d))
	if strings.Contains(out, "frase de sonda larga") {
		t.Fatal("la frase de restic-pass debe enmascararse")
	}
	if strings.Contains(out, "sonda-auth-secret-123") {
		t.Fatal("los secretos deben enmascararse")
	}
	if strings.Contains(out, "c29uZGEtY2xhdmU=") {
		t.Fatal("la clave maestra sin el prefijo base64: debe enmascararse")
	}
	if !strings.Contains(out, "user=flowsint") {
		t.Fatal("usuario/BD no son secretos: no deben enmascararse")
	}
}
