package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientBuildMergesOptions(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "install")

	path := filepath.Join(h.dir, "build", "client", "options.txt")
	seeded := "joinedFirstServer:true\nonboardAccessibility:false\nskipMultiplayerWarning:true\ntutorialStep:none\n"
	if data, err := os.ReadFile(path); err != nil || string(data) != seeded {
		t.Fatalf("expected the seeded options.txt, got %q, %v", data, err)
	}

	gameWritten := "version:4325\nfov:0.0\njoinedFirstServer:true\nlastServer:play.example.org:25565\nonboardAccessibility:false\nresourcePacks:[\"fabric\"]\nskipMultiplayerWarning:true\ntutorialStep:none\n"
	if err := os.WriteFile(path, []byte(gameWritten), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout := h.mustRun(t, "build")
	if !strings.Contains(stdout, "0 written, 2 unchanged, 0 kept") {
		t.Fatalf("rebuild after game write: %s", stdout)
	}
	if data, _ := os.ReadFile(path); string(data) != gameWritten {
		t.Fatalf("game-written file was changed: %q", data)
	}

	h.editManifest(t, func(m map[string]any) {
		m["client"] = map[string]any{"options": map[string]any{
			"tutorialStep":  "none",
			"fov":           0.5,
			"resourcePacks": `["fabric","sodium"]`,
		}}
	})
	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "2 written, 0 unchanged") {
		t.Fatalf("rebuild with changed keys: %s", stdout)
	}
	want := "version:4325\nfov:0.5\njoinedFirstServer:true\nlastServer:play.example.org:25565\nonboardAccessibility:false\nresourcePacks:[\"fabric\",\"sodium\"]\nskipMultiplayerWarning:true\ntutorialStep:none\n"
	if data, _ := os.ReadFile(path); string(data) != want {
		t.Fatalf("merged options.txt: %q", data)
	}
}

func TestClientBuildMergesIntoGameWrittenOptions(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")

	path := filepath.Join(h.dir, "build", "client", "options.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	gameWritten := "version:4325\ntutorialStep:movement\nskipMultiplayerWarning:false\njoinedFirstServer:false\nonboardAccessibility:false\n"
	if err := os.WriteFile(path, []byte(gameWritten), 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "install")
	want := "version:4325\ntutorialStep:none\nskipMultiplayerWarning:true\njoinedFirstServer:true\nonboardAccessibility:false\n"
	if data, _ := os.ReadFile(path); string(data) != want {
		t.Fatalf("merged into game-written options.txt: %q", data)
	}
	if stdout := h.mustRun(t, "build"); !strings.Contains(stdout, "0 written, 2 unchanged") {
		t.Fatalf("rebuild: %s", stdout)
	}
}
