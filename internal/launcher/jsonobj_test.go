package launcher

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadJSONObjectReportsAMissingFileAsEmpty(t *testing.T) {
	top, found, err := readJSONObject(filepath.Join(t.TempDir(), "instance.json"))
	if err != nil || found || top == nil || len(top) != 0 {
		t.Fatalf("missing file: top=%v found=%v err=%v", top, found, err)
	}
}

func TestReadJSONObjectDecodesANestedObject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instance.json")
	if err := os.WriteFile(path, []byte(`{"name":"cozy","launcher":{"enableCommands":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	top, found, err := readJSONObject(path)
	if err != nil || !found || jsonStringValue(top["name"]) != "cozy" {
		t.Fatalf("top=%v found=%v err=%v", top, found, err)
	}
	settings, err := jsonObjectAt(path, top, "launcher")
	if err != nil || string(settings["enableCommands"]) != "true" {
		t.Fatalf("settings=%v err=%v", settings, err)
	}
	absent, err := jsonObjectAt(path, top, "game_configuration")
	if err != nil || absent == nil || len(absent) != 0 {
		t.Fatalf("absent key: %v %v", absent, err)
	}
	if _, err := jsonObjectAt(path, top, "name"); err == nil {
		t.Fatal("a key holding no object must fail")
	}
}

func TestReadJSONObjectFailsOnAFileThatIsNoObject(t *testing.T) {
	path := filepath.Join(t.TempDir(), "instance.json")
	if err := os.WriteFile(path, []byte(`[1]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, found, err := readJSONObject(path); err == nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
}
