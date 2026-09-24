package loader

import (
	"context"
	"testing"
)

func TestNeoForgePrefix(t *testing.T) {
	cases := map[string]string{
		"1.21.1": "21.1.",
		"1.21":   "21.0.",
		"1.20.2": "20.2.",
		"26.2":   "26.2.0.",
		"26.1.2": "26.1.2.",
	}
	for game, want := range cases {
		if got, ok := neoforgePrefix(game); !ok || got != want {
			t.Errorf("%s: got %q, %v, want %q", game, got, ok, want)
		}
	}
	for _, game := range []string{"26.2-snapshot-3", "25w14craftmine", "1", "1.21.1.1", "26"} {
		if got, ok := neoforgePrefix(game); ok {
			t.Errorf("%s: got %q, want no prefix", game, got)
		}
	}
}

func TestNeoForgeVersionsAreFilteredByGame(t *testing.T) {
	r := fakeRemote(t, NeoForgeMavenURL, map[string]any{
		"/api/maven/versions/releases/net/neoforged/neoforge": map[string]any{"versions": []string{
			"21.1.200", "26.1.2.40", "26.2.0.56-beta", "26.2.0.87",
		}},
	})
	got, err := neoforge.Versions(context.Background(), r, "26.2")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (Version{"26.2.0.56-beta", false}) || got[1] != (Version{"26.2.0.87", true}) {
		t.Fatalf("versions %v", got)
	}
	if got, err := neoforge.Versions(context.Background(), r, "1.21.1"); err != nil || len(got) != 1 || got[0].Version != "21.1.200" {
		t.Fatalf("1.21.1: %v, %v", got, err)
	}
	if got, err := neoforge.Versions(context.Background(), r, "25w14craftmine"); err != nil || got != nil {
		t.Fatalf("snapshot: %v, %v", got, err)
	}
}
