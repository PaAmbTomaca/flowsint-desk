package snap

import "testing"

func TestParsePGCounts(t *testing.T) {
	m, err := parsePGCounts("profiles|2\nsketches|0\n\n")
	if err != nil {
		t.Fatal(err)
	}
	if m["profiles"] != 2 || m["sketches"] != 0 || len(m) != 2 {
		t.Fatalf("recuentos = %v", m)
	}
	for _, malo := range []string{"WARNING: algo", "profiles|x"} {
		if _, err := parsePGCounts(malo); err == nil {
			t.Fatalf("esperaba error para %q", malo)
		}
	}
}

func TestPGMismatch(t *testing.T) {
	want := map[string]int{"profiles": 2, "sketches": 5}
	if d := pgMismatch(want, map[string]int{"profiles": 2, "sketches": 5, "tabla_nueva": 9}); d != "" {
		t.Fatalf("una tabla extra tras restaurar no debe fallar: %s", d)
	}
	if d := pgMismatch(want, map[string]int{"profiles": 2, "sketches": 4}); d == "" {
		t.Fatal("recuento distinto debe fallar")
	}
	if d := pgMismatch(want, map[string]int{"profiles": 2}); d == "" {
		t.Fatal("tabla ausente debe fallar aunque el manifest diga 0 o más")
	}
	if d := pgMismatch(map[string]int{"logs": 0}, map[string]int{}); d == "" {
		t.Fatal("tabla ausente con manifest 0 debe fallar igualmente")
	}
}

func TestMapsEqual(t *testing.T) {
	if !mapsEqual(map[string]int{"a": 1}, map[string]int{"a": 1}) {
		t.Fatal("iguales")
	}
	if mapsEqual(map[string]int{"a": 1}, map[string]int{"a": 1, "b": 0}) {
		t.Fatal("distinto tamaño")
	}
}
