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
	l.Minecraft = "26.2"
	l.Loader = Loader{Type: "fabric", Version: "0.17.3"}
	l.Java = Java{Major: 25, Component: "java-runtime-epsilon"}
	if err := l.Save(filepath.Join(t.TempDir(), FileName)); err != nil {
		t.Fatal(err)
	}
}
