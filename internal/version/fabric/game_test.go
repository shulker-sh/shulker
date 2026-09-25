package fabric

import "testing"

func TestGame(t *testing.T) {
	for id, want := range map[string]string{
		"1.20.1":          "1.20.1",
		"26.2":            "26.2",
		"24w33a":          "1.21.2-alpha.24.33.a",
		"23w03a":          "1.19.4-alpha.23.3.a",
		"1.21-pre1":       "1.21-beta.1",
		"1.21-rc1":        "1.21-rc.1",
		"1.16-pre3":       "1.16-rc.3",
		"1.16-rc1":        "1.16-rc.9",
		"1.15.2-pre2":     "1.15.2-rc.2",
		"26.1-snapshot-1": "26.1-alpha.1",
		"26.1-pre-2":      "26.1-pre.2",
		"26.1-rc-1":       "26.1-rc.1",
		"26.1.1-rc-3":     "26.1.1-rc.3",
		"99w99z":          "99w99z",
	} {
		if got := Game(id); got != want {
			t.Errorf("Game(%q) = %q, want %q", id, got, want)
		}
	}
}
