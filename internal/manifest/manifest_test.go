package manifest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	for _, name := range []string{"two-target.json", "minimal.json"} {
		m, err := Load(filepath.Join("..", "..", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		data, err := m.Encode()
		if err != nil {
			t.Fatal(err)
		}
		again, err := Parse(data)
		if err != nil {
			t.Fatalf("%s: re-parse: %v", name, err)
		}
		h1, _ := m.ResolutionSha256()
		h2, _ := again.ResolutionSha256()
		if h1 != h2 {
			t.Fatalf("%s: hash changed across round trip", name)
		}
	}
}

func TestResolutionHashIgnoresNonResolutionFields(t *testing.T) {
	m, err := Load(filepath.Join("..", "..", "testdata", "two-target.json"))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := m.ResolutionSha256()
	m.Name = "renamed"
	m.Variables["motd"] = "changed"
	after, _ := m.ResolutionSha256()
	if before != after {
		t.Fatal("name and variables must not affect the resolution hash")
	}
	m.Mods["lithium"] = Mod{Channel: "beta"}
	if changed, _ := m.ResolutionSha256(); changed == before {
		t.Fatal("mods must affect the resolution hash")
	}
}

func TestSaveRefusesInvalid(t *testing.T) {
	m := &Manifest{Name: "x", Minecraft: "26.2", Loader: Loader{Type: "fabric", Version: "*"}, Targets: map[string]Target{}, Mods: map[string]Mod{}}
	if err := m.Save(filepath.Join(t.TempDir(), FileName)); err == nil {
		t.Fatal("expected schema error for empty targets")
	}
	m.Targets["client"] = Target{Side: "client", Overrides: []string{"overrides"}, Build: "build/client"}
	path := filepath.Join(t.TempDir(), FileName)
	if err := m.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
