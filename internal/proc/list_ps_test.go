//go:build !windows && !linux

package proc

import "testing"

func TestParsePSKeepsAPathWithSpaces(t *testing.T) {
	got := parsePS("  680 /usr/sbin/distnoted\n 4021 /Users/a/Applications/Prism Launcher.app/Contents/MacOS/prismlauncher\n\n")
	if got["680"] != "/usr/sbin/distnoted" || got["4021"] != "/Users/a/Applications/Prism Launcher.app/Contents/MacOS/prismlauncher" || len(got) != 2 {
		t.Fatalf("parsed %v", got)
	}
}
