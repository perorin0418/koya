package container

import (
	"strings"
	"testing"
)

func TestName(t *testing.T) {
	a := Name("python", "/home/u/src/My App")
	if !strings.HasPrefix(a, "koya-python-my-app-") || len(a) != len("koya-python-my-app-")+8 {
		t.Fatalf("unexpected name: %s", a)
	}
	if a == Name("python", "/other/My App") {
		t.Fatal("same basename in different dirs must differ")
	}
	if a != Name("python", "/home/u/src/My App") {
		t.Fatal("name must be stable")
	}
}

func TestOptionsKeyIsOrderIndependent(t *testing.T) {
	a := Options{Env: []string{"B=2", "A=1"}, Net: "none"}.Key()
	b := Options{Env: []string{"A=1", "B=2"}, Net: "none"}.Key()
	if a != b {
		t.Fatalf("%q != %q", a, b)
	}
	if (Options{}).Key() != "" {
		t.Fatal("empty options must give empty key")
	}
}

func TestParseInfoMissingTrailingField(t *testing.T) {
	info, ok := parseInfo("/koya-base-x-1\tbase\t/w\trunning\tabc")
	if !ok || info.Name != "koya-base-x-1" || info.Hash != "abc" || info.Opts != "" || !info.Running() {
		t.Fatalf("unexpected: %+v %v", info, ok)
	}
}
