package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPullToChoosesTheOverrideFolder(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	h.editManifest(t, func(m map[string]any) {
		m["features"] = map[string]any{
			"shaders": map[string]any{"default": true},
			"voice":   map[string]any{"default": true, "overrides": map[string]any{"client": "voice-client"}},
		}
	})
	writeOverride(t, h.dir, "overrides/config/plain.txt", "a=1\n")
	h.mustRun(t, "install")
	buildDir := filepath.Join(h.dir, "build", "client")
	read := func(rel string) string {
		data, _ := os.ReadFile(filepath.Join(h.dir, filepath.FromSlash(rel)))
		return string(data)
	}

	writeOverride(t, buildDir, "config/plain.txt", "a=2\n")
	h.mustRun(t, "pull", "--to", "client")
	if read("client-overrides/config/plain.txt") != "a=2\n" || read("overrides/config/plain.txt") != "a=1\n" {
		t.Fatalf("--to client writes the side folder and leaves the shared one: %q %q", read("client-overrides/config/plain.txt"), read("overrides/config/plain.txt"))
	}

	writeOverride(t, buildDir, "config/plain.txt", "a=3\n")
	h.mustRun(t, "pull")
	if read("client-overrides/config/plain.txt") != "a=3\n" || read("overrides/config/plain.txt") != "a=1\n" {
		t.Fatalf("a file is updated in the folder that already holds it: %q %q", read("client-overrides/config/plain.txt"), read("overrides/config/plain.txt"))
	}

	writeOverride(t, buildDir, "config/new.txt", "new\n")
	h.mustRun(t, "pull", "config/new.txt")
	if read("overrides/config/new.txt") != "new\n" {
		t.Fatalf("a new file lands in overrides/: %q", read("overrides/config/new.txt"))
	}

	writeOverride(t, buildDir, "config/shaders.txt", "s\n")
	h.mustRun(t, "pull", "config/shaders.txt", "--to", "shaders")
	if read("shaders-overrides/config/shaders.txt") != "s\n" {
		t.Fatalf("--to feature writes the feature's default folder: %q", read("shaders-overrides/config/shaders.txt"))
	}

	writeOverride(t, buildDir, "config/voice.txt", "v\n")
	h.mustRun(t, "pull", "config/voice.txt", "--to", "voice")
	if read("voice-client/config/voice.txt") != "v\n" {
		t.Fatalf("--to feature picks the object form's path for the side pulled from: %q", read("voice-client/config/voice.txt"))
	}

	writeOverride(t, buildDir, "config/voice.txt", "v2\n")
	code, stdout, _ := h.run(t, "pull", "--to", "nope", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "usage" || strings.Join(e.Candidates, ",") != "client,shaders,voice" {
		t.Fatalf("unknown --to: exit %d %s", code, stdout)
	}
}
