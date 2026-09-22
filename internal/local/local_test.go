package local

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/schema"
)

func TestSyncDirsAreKeyedBySide(t *testing.T) {
	f := &File{}
	if !f.RecordSyncDir("client", "/a") || f.RecordSyncDir("client", "/a") || !f.RecordSyncDir("client", "/b") || !f.RecordSyncDir("server", "/srv") {
		t.Fatalf("record: %+v", f.SyncDirs)
	}
	if len(f.SyncDirs["client"]) != 2 || len(f.SyncDirs["server"]) != 1 {
		t.Fatalf("sync dirs: %+v", f.SyncDirs)
	}
	if f.RemoveSyncDir("client", "/missing") || !f.RemoveSyncDir("client", "/a") || len(f.SyncDirs["client"]) != 1 {
		t.Fatalf("remove one: %+v", f.SyncDirs)
	}
	if !f.RemoveSyncDir("server", "/srv") {
		t.Fatal("remove the last server dir")
	}
	if _, ok := f.SyncDirs["server"]; ok {
		t.Fatalf("the side's key should go with its last entry: %+v", f.SyncDirs)
	}
}

func TestSaveMarksSchema(t *testing.T) {
	dir := t.TempDir()
	f, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	f.SetFeature("shaders", true)
	if err := f.Save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	var head struct {
		Schema string `json:"$schema"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		t.Fatal(err)
	}
	if want := "https://shulker.sh/schema/v1/local.json"; head.Schema != want {
		t.Fatalf("$schema = %q, want %q", head.Schema, want)
	}
	f, err = Load(dir)
	if err != nil || !f.Exists() || !f.Features["shaders"] {
		t.Fatalf("Load after Save = %+v, %v", f, err)
	}
}

func TestLoadReplacesAFileItCannotRead(t *testing.T) {
	const tail = "; moved it to %s and using the manifest's feature defaults"
	for _, c := range []struct {
		name, data, reason string
	}{
		{"newer", `{"$schema": "https://shulker.sh/schema/v2/local.json", "features": {"shaders": true}}`, "was written by a newer shulker (schema v2; this one reads up to v1)" + tail + ". Move it back after updating shulker to keep those settings"},
		{"missing", `{"features": {"shaders": true}}`, "is unreadable (names no $schema, which this shulker doesn't know)" + tail},
		{"foreign", `{"$schema": "https://shulker.sh/schema/v1/lock.json", "features": {"shaders": true}}`, "is unreadable (names the schema https://shulker.sh/schema/v1/lock.json, which this shulker doesn't know)" + tail},
		{"lower", `{"$schema": "https://shulker.sh/schema/v0/local.json", "features": {"shaders": true}}`, "is unreadable (names the schema https://shulker.sh/schema/v0/local.json, which this shulker doesn't know)" + tail},
		{"corrupt", `{"features": `, "is unreadable (unexpected end of JSON input)" + tail},
		{"wrong shape", `{"$schema": "https://shulker.sh/schema/v1/local.json", "features": {"shaders": "yes"}}`, "is unreadable (json: cannot unmarshal string into Go struct field File.features of type bool)" + tail},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, FileName)
			if err := os.WriteFile(path, []byte(c.data), 0o644); err != nil {
				t.Fatal(err)
			}
			f, err := Load(dir)
			var replaced *UnreadableError
			if !errors.As(err, &replaced) {
				t.Fatalf("err = %v, want a *UnreadableError", err)
			}
			if want := path + " " + fmt.Sprintf(c.reason, path+".replaced"); err.Error() != want {
				t.Errorf("err = %v\nwant %s", err, want)
			}
			if f == nil || f.Exists() || len(f.Features) != 0 || f.Dir() != dir {
				t.Fatalf("file = %+v, want an empty one for %s", f, dir)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("%s is still there: %v", FileName, err)
			}
			if kept, err := os.ReadFile(path + ".replaced"); err != nil || string(kept) != c.data {
				t.Errorf("replaced file = %q, %v; want the old bytes", kept, err)
			}
		})
	}
}

func TestLocalSchemaIsPublished(t *testing.T) {
	if _, err := schema.Raw(schema.Local); err != nil {
		t.Fatal(err)
	}
}

func TestLoadGoesOnWhenItCannotMoveTheFileAside(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	if err := os.WriteFile(path, []byte(`{"features": `), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	f, err := Load(dir)
	var replaced *UnreadableError
	if !errors.As(err, &replaced) || f == nil || f.Exists() {
		t.Fatalf("Load = %+v, %v; want an empty file and a *UnreadableError", f, err)
	}
	if want := path + " is unreadable (unexpected end of JSON input); couldn't move it aside, so using the manifest's feature defaults"; !strings.HasPrefix(err.Error(), want) {
		t.Errorf("err = %v\nwant it to start %s", err, want)
	}
}
