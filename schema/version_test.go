package schema

import (
	"testing"

	"shulker.sh/shulker/internal/out"
)

func TestURL(t *testing.T) {
	if got, want := URL(Accounts), "https://shulker.sh/schema/v1/accounts.json"; got != want {
		t.Errorf("URL(Accounts) = %q, want %q", got, want)
	}
}

func TestMarkerVersion(t *testing.T) {
	for _, c := range []struct {
		url, file string
		want      int
		ok        bool
	}{
		{"https://shulker.sh/schema/v1/accounts.json", "accounts.json", 1, true},
		{"https://shulker.sh/schema/v12/accounts.json", "accounts.json", 12, true},
		{"https://shulker.sh/schema/v1/registry.json", "accounts.json", 0, false},
		{"https://shulker.sh/schema/accounts.json", "accounts.json", 0, false},
		{"https://shulker.sh/schema/v0/accounts.json", "accounts.json", 0, false},
		{"https://example.com/v1/accounts.json", "accounts.json", 0, false},
		{"", "accounts.json", 0, false},
	} {
		got, ok := markerVersion(c.url, c.file)
		if got != c.want || ok != c.ok {
			t.Errorf("markerVersion(%q, %q) = %d, %v; want %d, %v", c.url, c.file, got, ok, c.want, c.ok)
		}
	}
}

func TestCheckMarker(t *testing.T) {
	const path = "/tmp/accounts.json"
	for _, c := range []struct {
		name, data, code string
		message          string
	}{
		{"current", `{"$schema":"https://shulker.sh/schema/v1/accounts.json"}`, "", ""},
		{
			"newer",
			`{"$schema":"https://shulker.sh/schema/v2/accounts.json"}`,
			"schema-newer",
			"/tmp/accounts.json was written by a newer shulker: its schema is v2, and this shulker knows v1",
		},
		{
			"older",
			`{"$schema":"https://shulker.sh/schema/v0/accounts.json"}`,
			"accounts-invalid",
			"/tmp/accounts.json: names the schema https://shulker.sh/schema/v0/accounts.json, which this shulker doesn't know",
		},
		{
			"another file",
			`{"$schema":"https://shulker.sh/schema/v1/registry.json"}`,
			"accounts-invalid",
			"/tmp/accounts.json: names the schema https://shulker.sh/schema/v1/registry.json, which this shulker doesn't know",
		},
		{
			"absent",
			`{}`,
			"accounts-invalid",
			"/tmp/accounts.json: names no $schema, which this shulker doesn't know",
		},
		{"unparseable", `{`, "accounts-invalid", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := CheckMarker(Accounts, "accounts-invalid", path, []byte(c.data))
			if c.code == "" {
				if err != nil {
					t.Fatalf("CheckMarker = %v, want nil", err)
				}
				return
			}
			if got := out.CodeOf(err); got != c.code {
				t.Fatalf("code = %q, want %q (%v)", got, c.code, err)
			}
			if c.message != "" && err.Error() != c.message {
				t.Errorf("message = %q, want %q", err.Error(), c.message)
			}
		})
	}
}

func TestCheckMarkerNewerNudgesSelfUpdate(t *testing.T) {
	err := CheckMarker(Accounts, "accounts-invalid", "/tmp/accounts.json", []byte(`{"$schema":"https://shulker.sh/schema/v2/accounts.json"}`))
	e := out.AsError(err)
	if e.Nudge.Command != "shulker self update" {
		t.Errorf("nudge = %q, want `shulker self update`", e.Nudge.Command)
	}
}
