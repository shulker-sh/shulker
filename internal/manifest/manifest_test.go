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

func TestConditionsRoundTripAndSkipTheResolutionHash(t *testing.T) {
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
	before, _ := m.ResolutionSha256()
	m.Mods["aa"] = Mod{OS: StringList{"linux"}, Feature: StringList{"x"}}
	if after, _ := m.ResolutionSha256(); after != before {
		t.Fatal("conditions must not affect the resolution hash")
	}
}
