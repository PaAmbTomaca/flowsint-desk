package passphrase

import (
	"fmt"
	"testing"

	"github.com/zalando/go-keyring"
)

// TestKeyringProbe usa una clave de SONDA: nunca toca la clave real
// ("flowsint-desk"/"passphrase") que init guarda en el llavero.
func TestKeyringProbe(t *testing.T) {
	const svc, acc = "flowsint-desk-probe", "probe"
	if err := keyring.Set(svc, acc, "valor-de-prueba"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, err := keyring.Get(svc, acc)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	fmt.Printf("probe Set/Get OK (len=%d)\n", len(v))
	if err := keyring.Delete(svc, acc); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := keyring.Get(svc, acc); err == nil {
		t.Fatal("tras borrar la sonda, Get debería fallar")
	}
	fmt.Println("probe Delete OK (clave real intacta)")
}
