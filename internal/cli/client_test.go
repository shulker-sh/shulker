package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientBuildMergesOptions(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
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
	if !strings.Contains(stdout, "built client (2 unchanged)") {
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
	if !strings.Contains(stdout, "built client (2 written)") {
		t.Fatalf("rebuild with changed keys: %s", stdout)
	}
	want := "version:4325\nfov:0.5\nlastServer:play.example.org:25565\nresourcePacks:[\"fabric\",\"sodium\"]\ntutorialStep:none\n"
	if data, _ := os.ReadFile(path); string(data) != want {
		t.Fatalf("merged options.txt: %q", data)
	}
}

func TestClientBuildMergesIntoGameWrittenOptions(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")

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
	if stdout := h.mustRun(t, "build"); !strings.Contains(stdout, "built client (2 unchanged)") {
		t.Fatalf("rebuild: %s", stdout)
	}
}

func TestClientBuildRestoresDroppedOptions(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.editManifest(t, func(m map[string]any) {
		m["client"] = map[string]any{"options": map[string]any{"key_zoomify.key.zoom": "key.keyboard.z", "fov": 0.5}}
	})
	h.mustRun(t, "install")
	path := filepath.Join(h.dir, "build", "client", "options.txt")

	gameWritten := "version:4325\nfov:0.5\nlastServer:play.example.org:25565\n"
	if err := os.WriteFile(path, []byte(gameWritten), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout := h.mustRun(t, "build")
	if !strings.Contains(stdout, "built client (1 written, 1 unchanged)") {
		t.Fatalf("rebuild after the game dropped a key: %s", stdout)
	}
	if data, _ := os.ReadFile(path); string(data) != gameWritten+"key_zoomify.key.zoom:key.keyboard.z\n" {
		t.Fatalf("dropped key not restored: %q", data)
	}

	if err := os.WriteFile(path, []byte("version:4325\nfov:0.5\nlastServer:play.example.org:25565\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.editManifest(t, func(m map[string]any) {
		m["client"].(map[string]any)["options"].(map[string]any)["key_zoomify.key.zoom"] = "key.keyboard.v"
	})
	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "built client (2 written)") {
		t.Fatalf("dropped in build and changed in manifest must just write: %s", stdout)
	}
	if data, _ := os.ReadFile(path); !strings.HasSuffix(string(data), "key_zoomify.key.zoom:key.keyboard.v\n") {
		t.Fatalf("new value not written: %q", data)
	}
}
