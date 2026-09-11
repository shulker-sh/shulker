package cli

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/resolve"
)

func TestSuggestsKeepsOptionalIntegrationsBehindAFlag(t *testing.T) {
	h := newHarness(t)
	h.jars["sodium"] = makeJarFile(t, "sodium", h.jars["sodium"].filename, "quilt.mod.json",
		`{"schema_version":1,"quilt_loader":{"id":"sodium","version":"1.0.0","depends":[{"id":"iris","versions":"^1.8","optional":true},{"id":"modmenu","optional":true}]}}`)
	h.jars["fabric-api"] = makeJarWith(t, "fabric-api", h.jars["fabric-api"].filename, "*", `"depends":{"fabricloader":">=0.17"},"recommends":{"indium":"*"}`)
	h.mustRun(t, "init", "--yes")

	stdout := h.mustRun(t, "add", "sodium", "fabric-api")
	if !strings.Contains(stdout, "fabric-api recommends indium (not installed)") || !strings.Contains(stdout, "2 optional integrations; see shulker suggests --optional") {
		t.Fatalf("add output:\n%s", stdout)
	}

	decode := func(stdout string) []resolve.Suggestion {
		t.Helper()
		var env struct {
			Data struct {
				Suggestions []resolve.Suggestion `json:"suggestions"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(stdout), &env); err != nil {
			t.Fatalf("%v: %s", err, stdout)
		}
		return env.Data.Suggestions
	}
	indium := resolve.Suggestion{Mod: "fabric-api", Kind: "recommends", On: "indium", Declared: "*"}
	if got := decode(h.mustRun(t, "suggests", "--json")); !reflect.DeepEqual(got, []resolve.Suggestion{indium}) {
		t.Fatalf("suggests: %+v", got)
	}
	want := []resolve.Suggestion{
		indium,
		{Mod: "sodium", Kind: "optional", On: "iris", Declared: "^1.8"},
		{Mod: "sodium", Kind: "optional", On: "modmenu", Declared: "*"},
	}
	if got := decode(h.mustRun(t, "suggests", "--optional", "--json")); !reflect.DeepEqual(got, want) {
		t.Fatalf("suggests --optional: %+v", got)
	}

	stdout = h.mustRun(t, "suggests")
	if want := "fabric-api\n  recommends indium\n2 optional integrations; see shulker suggests --optional\n"; stdout != want {
		t.Fatalf("suggests text:\n%s", stdout)
	}
	stdout = h.mustRun(t, "suggests", "--optional")
	if !strings.Contains(stdout, "sodium\n  optional iris ^1.8\n  optional modmenu\n") {
		t.Fatalf("suggests --optional text:\n%s", stdout)
	}
}
