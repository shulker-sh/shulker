package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"shulker.sh/shulker/schema"
)

func TestWriteStateMarksSchema(t *testing.T) {
	dir := t.TempDir()
	if err := writeState(dir, State{Files: map[string]string{"mods/a.jar": "abc"}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(StatePath(dir))
	if err != nil {
		t.Fatal(err)
	}
	var head struct {
		Schema string `json:"$schema"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		t.Fatal(err)
	}
	if want := "https://shulker.sh/schema/v1/state.json"; head.Schema != want {
		t.Fatalf("$schema = %q, want %q", head.Schema, want)
	}
	s, err := ReadState(dir)
	if err != nil || s.Files["mods/a.jar"] != "abc" {
		t.Fatalf("ReadState after writeState = %+v, %v", s, err)
	}
}

func TestReadStateMarker(t *testing.T) {
	for _, c := range []struct {
		name, schema, reason string
	}{
		{"newer", "https://shulker.sh/schema/v2/state.json", "was written by a newer shulker (schema v2; this one knows v1); treating every file as not written by shulker. Run `shulker self update` to read it, or rebuild with --force to take them over"},
		{"missing", "", "is unreadable (names no $schema, which this shulker doesn't know); treating every file as not written by shulker. Rebuild with --force to take them over"},
		{"foreign", "https://shulker.sh/schema/v1/lock.json", "is unreadable (names the schema https://shulker.sh/schema/v1/lock.json, which this shulker doesn't know); treating every file as not written by shulker. Rebuild with --force to take them over"},
		{"lower", "https://shulker.sh/schema/v0/state.json", "is unreadable (names the schema https://shulker.sh/schema/v0/state.json, which this shulker doesn't know); treating every file as not written by shulker. Rebuild with --force to take them over"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			doc := map[string]any{"side": "client", "files": map[string]string{"mods/a.jar": "abc"}}
			if c.schema != "" {
				doc["$schema"] = c.schema
			}
			data, _ := json.Marshal(doc)
			if err := os.MkdirAll(filepath.Join(dir, StateDir), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(StatePath(dir), data, 0o644); err != nil {
				t.Fatal(err)
			}
			s, err := ReadState(dir)
			if len(s.Files) != 0 {
				t.Errorf("files = %v, want none", s.Files)
			}
			if want := StatePath(dir) + " " + c.reason; err == nil || err.Error() != want {
				t.Errorf("err = %v\nwant %s", err, want)
			}
		})
	}
}

func TestStateSchemaIsPublished(t *testing.T) {
	if _, err := schema.Raw(schema.State); err != nil {
		t.Fatal(err)
	}
}
