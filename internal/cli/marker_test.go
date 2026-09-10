package cli

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientBuildWritesMarkerJar(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "my.pack")
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "install")

	jarPath := filepath.Join(h.dir, "build", "client", "mods", "shulker-my.pack.jar")
	first, err := os.ReadFile(jarPath)
	if err != nil {
		t.Fatal(err)
	}
	entries := readZip(t, first)
	var meta struct {
		SchemaVersion int    `json:"schemaVersion"`
		ID            string `json:"id"`
		Version       string `json:"version"`
		Name          string `json:"name"`
		Description   string `json:"description"`
		Icon          string `json:"icon"`
		Environment   string `json:"environment"`
		Custom        struct {
			ModMenu struct {
				UpdateChecker bool `json:"update_checker"`
			} `json:"modmenu"`
		} `json:"custom"`
	}
	if err := json.Unmarshal(entries["fabric.mod.json"], &meta); err != nil {
		t.Fatal(err)
	}
	if meta.SchemaVersion != 1 || meta.ID != "shulker_my_pack" || meta.Name != "my.pack" || meta.Environment != "*" || len(meta.Version) != 8 || meta.Custom.ModMenu.UpdateChecker {
		t.Fatalf("fabric.mod.json: %+v", meta)
	}
	want := "Built by shulker: Minecraft 26.2, fabric 0.17.3, 2 mods.\n\nMods:\nsodium 1.0.0+mc26.2\n\nDependencies:\nfabric-api 1.0.0+mc26.2"
	if meta.Description != want {
		t.Fatalf("description:\n%s", meta.Description)
	}
	if meta.Icon != "assets/shulker_my_pack/icon.png" || len(entries[meta.Icon]) == 0 {
		t.Fatalf("icon %q missing from jar", meta.Icon)
	}
	var embedded map[string]any
	if err := json.Unmarshal(entries["shulker.lock"], &embedded); err != nil || embedded["minecraft"] != "26.2" {
		t.Fatalf("embedded lock: %v %v", err, embedded)
	}
	if err := json.Unmarshal(entries["shulker.json"], &embedded); err != nil || embedded["name"] != "my.pack" {
		t.Fatalf("embedded manifest: %v %v", err, embedded)
	}

	stdout := h.mustRun(t, "build")
	if !strings.Contains(stdout, "0 written, 3 unchanged") {
		t.Fatalf("rebuild should be a no-op: %s", stdout)
	}
	second, _ := os.ReadFile(jarPath)
	if !bytes.Equal(first, second) {
		t.Fatal("marker jar is not byte-stable across builds")
	}
}

func readZip(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		entries[f.Name], _ = io.ReadAll(rc)
		rc.Close()
	}
	return entries
}
