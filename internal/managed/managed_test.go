package managed

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/schema"
)

type accounts struct {
	Schema   string           `json:"$schema"`
	Accounts []map[string]any `json:"accounts,omitempty"`
}

func write(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "accounts.json")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadChecksTheMarkerBeforeTheSchema(t *testing.T) {
	own := schema.URL(schema.Accounts)
	for _, c := range []struct{ name, data, code, says, cause string }{
		{"own", `{"$schema":"` + own + `"}`, "", "", ""},
		{"newer", `{"$schema":"https://shulker.sh/schema/v2/accounts.json","future":true}`, "schema-newer", "was written by a newer shulker (schema v2; this one reads up to v1)", "was written by a newer shulker (schema v2; this one reads up to v1)"},
		{"foreign", `{"$schema":"https://example.com/accounts.json","future":true}`, "accounts-invalid", "names the schema https://example.com/accounts.json, which this shulker doesn't know", "names the schema https://example.com/accounts.json, which this shulker doesn't know"},
		{"absent", `{"future":true}`, "accounts-invalid", "names no $schema, which this shulker doesn't know", "names no $schema, which this shulker doesn't know"},
		{"corrupt", `{"$schema": `, "accounts-invalid", "the file ends before the value is complete", "unexpected end of JSON input"},
		{"invalid", `{"$schema":"` + own + `","accounts":{}}`, "accounts-invalid", "accounts: got object, want array", "got object, want array"},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := write(t, c.data)
			var v accounts
			err := Read(schema.Accounts, path, &v)
			if c.code == "" {
				if err != nil || v.Schema != own {
					t.Fatalf("Read = %+v, %v", v, err)
				}
				return
			}
			e := out.AsError(err)
			if err == nil || e.Code != c.code || !strings.HasPrefix(e.Message, path) || !strings.Contains(e.Message, c.says) {
				t.Fatalf("Read = %v (code %s), want %s saying %q", err, e.Code, c.code, c.says)
			}
			if e.Cause == nil || !strings.Contains(e.Cause.Error(), c.cause) {
				t.Errorf("cause = %v, want %q", e.Cause, c.cause)
			}
			if (c.code == "schema-newer") != (e.Nudge == schema.UpdateNudge) {
				t.Errorf("nudge = %+v", e.Nudge)
			}
		})
	}
}

func TestReadTakesAnEmptyFileAsZeroWhereTheKindAllows(t *testing.T) {
	v := accounts{Schema: "untouched"}
	if err := Read(schema.Accounts, write(t, " \n"), &v); err != nil || v.Schema != "untouched" {
		t.Fatalf("Read = %+v, %v", v, err)
	}
	var doc map[string]any
	err := Read(schema.Config, write(t, ""), &doc)
	if e := out.AsError(err); err == nil || e.Code != "config-invalid" || !strings.HasSuffix(e.Message, "the file ends before the value is complete") {
		t.Fatalf("Read = %v", err)
	}
}

func TestReadLeavesAMissingFileToTheCaller(t *testing.T) {
	var v accounts
	if err := Read(schema.Accounts, filepath.Join(t.TempDir(), "accounts.json"), &v); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Read = %v, want fs.ErrNotExist", err)
	}
}

func TestReadSkipsTheSchemaForAKindThatDoesNotValidate(t *testing.T) {
	path := write(t, `{"$schema":"`+schema.URL(schema.Config)+`","registry":7}`)
	var doc map[string]any
	if err := Read(schema.Config, path, &doc); err != nil {
		t.Fatalf("Read = %v", err)
	}
	if doc["registry"].(interface{ String() string }).String() != "7" {
		t.Fatalf("numbers are kept exact, got %T", doc["registry"])
	}
}

func TestDecodeReadsAMarkerlessManifestAsCurrent(t *testing.T) {
	var m struct{ Name string }
	if err := Decode(schema.Manifest, "shulker.json", []byte(`{"name":"p","client":{}}`), &m); err != nil || m.Name != "p" {
		t.Fatalf("Decode = %+v, %v", m, err)
	}
	err := Decode(schema.Manifest, "shulker.json", []byte(`{"$schema":"https://example.com/x.json","name":"p","client":{}}`), &m)
	if e := out.AsError(err); err == nil || e.Code != "manifest-invalid" || !strings.HasPrefix(e.Message, "shulker.json: names the schema") {
		t.Fatalf("Decode = %v", err)
	}
}

func TestReplaceKeepsTheOldFileAndWritesOneItCanRead(t *testing.T) {
	path := write(t, `{"old": true}`)
	kept, err := Replace(schema.Accounts, path, accounts{Schema: schema.URL(schema.Accounts)})
	if err != nil || kept != path+".replaced" {
		t.Fatalf("Replace = %q, %v", kept, err)
	}
	if old, _ := os.ReadFile(kept); string(old) != `{"old": true}` {
		t.Errorf("kept = %s", old)
	}
	var v accounts
	if err := Read(schema.Accounts, path, &v); err != nil {
		t.Fatalf("the replacement reads back: %v", err)
	}
}

func TestReplaceRefusesAFileItCouldNotReadBack(t *testing.T) {
	path := write(t, `{"old": true}`)
	for name, v := range map[string]any{
		"unmarked": accounts{},
		"invalid":  map[string]any{"$schema": schema.URL(schema.Accounts), "accounts": 1},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Replace(schema.Accounts, path, v)
			if e := out.AsError(err); err == nil || e.Code != "accounts-invalid" || !strings.HasPrefix(e.Message, "refusing to write "+path) {
				t.Fatalf("Replace = %v", err)
			}
			if old, _ := os.ReadFile(path); string(old) != `{"old": true}` {
				t.Errorf("the old file was touched: %s", old)
			}
		})
	}
}

func TestMoveAsideKeepsTheOldFileAndWritesNothing(t *testing.T) {
	path := write(t, `{"old": true}`)
	kept, err := MoveAside(path)
	if err != nil || kept != path+".replaced" {
		t.Fatalf("MoveAside = %q, %v", kept, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the file is still there: %v", err)
	}
	if kept, err := MoveAside(path); err != nil || kept != "" {
		t.Errorf("a missing file: %q, %v", kept, err)
	}
}
