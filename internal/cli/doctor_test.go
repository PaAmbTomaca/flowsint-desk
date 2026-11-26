package cli

import (
	"reflect"
	"testing"
)

func TestNonZramSwaps(t *testing.T) {
	soloZram := "Filename\t\t\t\tType\t\tSize\t\tUsed\t\tPriority\n/dev/zram0                              partition\t32534524\t1277164\t\t100\n"
	if got := nonZramSwaps(soloZram); len(got) != 0 {
		t.Fatalf("solo zram → %v", got)
	}
	mixto := soloZram + "/dev/nvme0n1p3                          partition\t8388604\t\t0\t\t-2\n/swapfile                               file\t\t4194300\t\t0\t\t-3\n"
	want := []string{"/dev/nvme0n1p3", "/swapfile"}
	if got := nonZramSwaps(mixto); !reflect.DeepEqual(got, want) {
		t.Fatalf("mixto → %v, esperaba %v", got, want)
	}
	if got := nonZramSwaps("Filename Type Size Used Priority\n"); len(got) != 0 {
		t.Fatalf("sin swap → %v", got)
	}
}

func TestParseVersionYKernel(t *testing.T) {
	if maj, min, ok := parseVersion("0.19.1"); !ok || maj != 0 || min != 19 {
		t.Fatalf("parseVersion = %d %d %v", maj, min, ok)
	}
	if _, _, ok := parseVersion("basura"); ok {
		t.Fatal("parseVersion debe rechazar basura")
	}
	if maj, min := parseKernel("6.18.44-1-cachyos"); maj != 6 || min != 18 {
		t.Fatalf("parseKernel = %d %d", maj, min)
	}
}

func TestSumFor(t *testing.T) {
	h1 := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	h2 := "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	sums := h1 + "  restic_0.19.1_linux_386.bz2\n" + h2 + "  restic_0.19.1_linux_amd64.bz2\n"
	got, err := sumFor(sums, "restic_0.19.1_linux_amd64.bz2")
	if err != nil || got != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("sumFor = %q, %v", got, err)
	}
	if _, err := sumFor(sums, "restic_0.19.1_linux_arm64.bz2"); err == nil {
		t.Fatal("fichero ausente debe dar error")
	}
	if _, err := sumFor("corto  restic_0.19.1_linux_amd64.bz2\n", "restic_0.19.1_linux_amd64.bz2"); err == nil {
		t.Fatal("hash truncado debe dar error")
	}
}
