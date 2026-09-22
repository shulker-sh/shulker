package schema

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the generated pages in site/docs")

func TestDocsPagesMatchSchemas(t *testing.T) {
	for _, page := range docsPages {
		got, err := renderDocsPage(page)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join("..", "site", "docs", page.name)
		if *update {
			if err := os.WriteFile(path, got, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("site/docs/%s is out of date with schema/%s; run: go test ./schema -run TestDocs -update", page.name, page.kind)
		}
	}
}

func describeJSON(t *testing.T, src string) string {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(src))
	dec.UseNumber()
	v, err := decodeOrdered(dec)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := describe(v.(*object), "")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(lines, "\n")
}

func TestDescribeStatesObjectRules(t *testing.T) {
	got := describeJSON(t, `{
		"type": "object",
		"properties": {"source": {"type": "string"}, "ref": {"type": "string"}, "locked": {"type": "boolean"},
			"type": {"type": "string"}, "file": {"type": "string"}, "pin": {"type": "string"}},
		"anyOf": [{"required": ["source"]}, {"required": ["file"]}],
		"dependentRequired": {"ref": ["source"], "locked": ["source"], "pin": ["type", "file"]},
		"allOf": [
			{"if": {"required": ["source"]}, "then": {"properties": {"type": {"const": "modpack"}},
				"not": {"anyOf": [{"required": ["file"]}, {"required": ["pin"]}]}}},
			{"if": {"required": ["file"]}, "then": {"required": ["type"]}},
			{"if": {"required": ["file", "type"], "properties": {"type": {"const": "modpack"}}},
				"then": {"not": {"anyOf": [{"required": ["pin"]}]}}},
			{"if": {"required": ["locked"]}, "then": {"anyOf": [{"required": ["source"]}, {"required": ["file"]}]}}
		]
	}`)
	for _, want := range []string{
		"At least one of `source` or `file` is required.",
		"`ref` and `locked` require `source`.",
		"`pin` requires `type` and `file`.",
		"When `source` is set, `type` must be `\"modpack\"`, and `file` and `pin` are not allowed.",
		"When `file` is set, `type` is required.",
		"When `file` is set and `type` is `\"modpack\"`, `pin` is not allowed.",
		"When `locked` is set, `source` or `file` is required.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestDescribeRejectsRulesItCannotState(t *testing.T) {
	dec := json.NewDecoder(strings.NewReader(`{"properties": {"a": {"type": "string"}}, "allOf": [{"if": {"properties": {"a": {"const": "x"}}}, "then": {"required": ["a"]}}]}`))
	v, err := decodeOrdered(dec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := describe(v.(*object), ""); err == nil {
		t.Fatal("describe dropped an allOf branch it cannot state")
	}
}

func TestDescribeListsNestedPropertiesAsDottedRows(t *testing.T) {
	got := describeJSON(t, `{
		"type": "object",
		"properties": {
			"settings": {"type": "object", "required": ["memory"], "properties": {
				"hooks": {"type": "object", "properties": {"preLaunch": {"type": "boolean", "default": true}}},
				"memory": {"type": "string"}
			}},
			"servers": {"type": "array", "items": {"type": "object", "properties": {"ip": {"type": "string"}}}},
			"ops": {"type": "array", "items": {"allOf": [{"$ref": "#/$defs/player"}, {"properties": {"level": {"type": "integer"}}}]}},
			"after": {"type": "string"}
		}
	}`)
	want := strings.Join([]string{
		"| `settings` | object |  |",
		"| `settings.hooks` | object |  |",
		"| `settings.hooks.preLaunch` | `boolean` | default `true` |",
		"| `settings.memory` * | `string` |  |",
		"| `servers` | object[] |  |",
		"| `servers[].ip` | `string` |  |",
		"| `ops` | [`player`](#player)[] |  |",
		"| `ops[].level` | `integer` |  |",
		"| `after` | `string` |  |",
	}, "\n")
	if !strings.Contains(got, want) {
		t.Errorf("want rows:\n%s\ngot:\n%s", want, got)
	}
}
