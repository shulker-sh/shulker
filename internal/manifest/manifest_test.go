package manifest

import (
	"os"
	"path/filepath"
	"strings"
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
		if _, err := Parse(data); err != nil {
			t.Fatalf("%s: re-parse: %v", name, err)
		}
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

func TestConditionsRoundTrip(t *testing.T) {
	m, err := Parse([]byte(`{"name":"p","minecraft":"26.2","loader":{"type":"fabric","version":"*"},"targets":{"client":{"side":"client","overrides":["overrides"],"features":["fancy"]}},"mods":{"aa":{"os":"macos"},"bb":{"feature":["fancy","!shaders"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Mods["aa"].OS) != 1 || m.Mods["aa"].OS[0] != "macos" || len(m.Mods["bb"].Feature) != 2 || m.Targets["client"].Features[0] != "fancy" {
		t.Fatalf("parsed conditions: %+v", m.Mods)
	}
	data, err := m.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if s := string(data); !strings.Contains(s, `"os": "macos"`) || !strings.Contains(s, "\"feature\": [\n") {
		t.Fatalf("encoded conditions: %s", s)
	}
}
