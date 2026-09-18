package schema

import (
	"strings"
	"testing"
)

func TestInvalidNamesLineColumnAndPlainWords(t *testing.T) {
	for _, c := range []struct{ data, want string }{
		{"{\n  \"a\": 1,\n}\n", "f.json:3:1: trailing comma before }"},
		{"{\"a\": [1, 2,]}", "f.json:1:13: trailing comma before ]"},
		{"{\n  \"a\": 1", "f.json:2:8: the file ends before the value is complete"},
		{"", "f.json is empty"},
		{"  \n", "f.json is empty"},
		{"{\n  \"a\" 1\n}", "f.json:2:7: invalid character '1' after a key; expected :"},
		{"{\n  \"a\": 1\n  \"b\": 2\n}", "f.json:3:3: invalid character '\"' after a value; expected , or }"},
		{"[1 2]", "f.json:1:4: invalid character '2' after a list item; expected , or ]"},
		{"{\n  a: 1\n}", "f.json:2:3: invalid character 'a' where a quoted key should start"},
		{"{\"a\": tru}", "f.json:1:10: invalid character '}' in literal true (expecting 'e')"},
		{"{\"a\": \"x\ny\"}", "f.json:1:9: invalid character '\\n' inside a string; escape it as \\n or close the string first"},
		{"{\"a\": 1}\n{}", "f.json:2:1: extra content after the closing brace"},
	} {
		_, err := Decode([]byte(c.data))
		if err == nil {
			t.Fatalf("%q should not parse", c.data)
		}
		if got := Invalid("x-invalid", "f.json", []byte(c.data), err); got.Code != "x-invalid" || got.Message != c.want {
			t.Errorf("%q\n got %s\nwant %s", c.data, got.Message, c.want)
		}
	}
}

func TestInvalidListsSchemaProblemsByPath(t *testing.T) {
	data := []byte(`{"name": "x", "minecraft": "26.2", "loader": {"version": "*"}, "client": {"build": 7}, "requires": {}}`)
	err := Validate(Manifest, data)
	if err == nil {
		t.Fatal("expected schema failure")
	}
	e := Invalid("manifest-invalid", "shulker.json", data, err)
	if e.Message != "shulker.json has 2 problems" || len(e.Items) != 2 || e.Items[0] != "client.build: got number, want string" || e.Items[1] != "loader: missing property 'type'" {
		t.Fatalf("two problems: %q %v", e.Message, e.Items)
	}
	data = []byte(`{"name": "x", "minecraft": "26.2", "loader": {"type": "fabric", "version": "0.17.3"}, "client": {"build": 7}, "requires": {}}`)
	e = Invalid("manifest-invalid", "shulker.json", data, Validate(Manifest, data))
	if len(e.Items) != 0 || !strings.HasPrefix(e.Message, "shulker.json: client.build: got number, want string") {
		t.Fatalf("one problem: %q %v", e.Message, e.Items)
	}
}
