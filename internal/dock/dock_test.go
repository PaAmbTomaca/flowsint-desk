package dock

import (
	"os"
	"reflect"
	"testing"
)

func TestImagesUsaLaVersion(t *testing.T) {
	im := Images("1.2.10")
	if im[len(im)-2] != "ghcr.io/reconurge/flowsint-api:1.2.10" || im[len(im)-1] != "ghcr.io/reconurge/flowsint-app:1.2.10" {
		t.Fatalf("imágenes = %v", im)
	}
	if def := Images(""); def[len(def)-1] != "ghcr.io/reconurge/flowsint-app:latest" {
		t.Fatalf("por defecto latest: %v", def)
	}
}

func TestDrift(t *testing.T) {
	a := "sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	b := "sha256:" + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	want := map[string]string{"api:latest": "ghcr.io/x/api@" + a, "app:latest": "ghcr.io/x/app@" + a, "neo4j:5": "", "redis:7": "redis@" + a}
	have := map[string]string{"api:latest": "ghcr.io/x/api@" + b, "app:latest": "otro-mirror/app@" + a, "neo4j:5": "neo4j@" + b, "redis:7": ""}
	// api difiere; app es el mismo digest aunque cambie el repo; sin dato en un lado no cuenta
	if got := Drift(want, have); !reflect.DeepEqual(got, []string{"api:latest"}) {
		t.Fatalf("Drift = %v", got)
	}
}

func TestCachedDigestsYDropCache(t *testing.T) {
	d := t.TempDir()
	img := "ghcr.io/reconurge/flowsint-api:latest"
	if err := os.WriteFile(digestFile(d, img), []byte("ghcr.io/reconurge/flowsint-api@sha256:abc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := CachedDigests(d, []string{img, "alpine:3"})
	if got[img] != "ghcr.io/reconurge/flowsint-api@sha256:abc" || got["alpine:3"] != "" {
		t.Fatalf("CachedDigests = %v", got)
	}
	DropCache(d, img)
	if CachedDigests(d, []string{img})[img] != "" {
		t.Fatal("DropCache debe borrar el digest")
	}
}

func TestTailFile(t *testing.T) {
	p := t.TempDir() + "/log"
	_ = os.WriteFile(p, []byte("a\nb\nc\nd\n"), 0o600)
	if got := tailFile(p, 2); got != "c\nd" {
		t.Fatalf("tailFile = %q", got)
	}
}
