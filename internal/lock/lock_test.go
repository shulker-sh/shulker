package lock

import (
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	l, err := Load(filepath.Join("..", "..", "testdata", "lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := l.Encode()
	if err != nil {
		t.Fatal(err)
	}
	again, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if again.Mods["betterthirdperson"].URL != nil || again.Mods["betterthirdperson"].Page == "" {
		t.Fatal("null url and page must survive a round trip")
	}
	if again.Mods["appleskin"].Aliases.CurseForge != 248787 {
		t.Fatal("aliases must survive a round trip")
	}
}

func TestNewSaves(t *testing.T) {
	l := New()
	l.ManifestSha256 = "5d8a1f0c3b7e9a2d4c6f8e0b1a3d5c7e9f0a2b4c6d8e0f1a3b5c7d9e1f2a4b6c"
	l.Minecraft = "26.2"
	l.Loader = Loader{Type: "fabric", Version: "0.17.3"}
	l.Java = Java{Major: 25, Component: "java-runtime-epsilon"}
	if err := l.Save(filepath.Join(t.TempDir(), FileName)); err != nil {
		t.Fatal(err)
	}
}
