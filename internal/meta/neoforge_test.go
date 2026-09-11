package meta

import "testing"

func TestNeoForgePrefix(t *testing.T) {
	cases := map[string]string{
		"1.21.1": "21.1.",
		"1.21":   "21.0.",
		"1.20.2": "20.2.",
		"26.2":   "26.2.0.",
		"26.1.2": "26.1.2.",
	}
	for game, want := range cases {
		if got, ok := neoForgePrefix(game); !ok || got != want {
			t.Errorf("%s: got %q, %v, want %q", game, got, ok, want)
		}
	}
	for _, game := range []string{"26.2-snapshot-3", "25w14craftmine", "1", "1.21.1.1", "26"} {
		if got, ok := neoForgePrefix(game); ok {
			t.Errorf("%s: got %q, want no prefix", game, got)
		}
	}
}
