package passphrase

import (
	"testing"

	"github.com/zalando/go-keyring"
)

// TestKeyringProbe usa una clave de SONDA: nunca toca la clave real
// ("flowsint-desk"/"passphrase") que init guarda en el llavero.
// Sin Secret Service (CI, sesión sin D-Bus) se salta: el llavero es opcional.
func TestKeyringProbe(t *testing.T) {
	if !KeyringAvailable() {
		t.Skip("sin Secret Service en este entorno: el llavero es opcional")
	}
	const svc, acc = "flowsint-desk-probe", "probe"
	if err := keyring.Set(svc, acc, "valor-de-prueba"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, err := keyring.Get(svc, acc)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v != "valor-de-prueba" {
		t.Fatalf("Get devolvió un valor distinto (len=%d)", len(v))
	}
	if err := keyring.Delete(svc, acc); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := keyring.Get(svc, acc); err == nil {
		t.Fatal("tras borrar la sonda, Get debería fallar")
	}
}
