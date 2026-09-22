package cli

import (
	"encoding/json"
	"path/filepath"
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
	h.mustRun(t, "init", "--yes", "--loader", "quilt")

	stdout := h.mustRun(t, "add", "sodium", "fabric-api")
	if !strings.Contains(stdout, "• fabric-api (recommends indium, not installed)") || !strings.Contains(stdout, "2 optional integrations to see:\n    $ shulker suggests --optional") {
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
	if want := "  • fabric-api (recommends indium)\n\n  2 optional integrations to see:\n    $ shulker suggests --optional\n"; stdout != want {
		t.Fatalf("suggests text:\n%s", stdout)
	}
	stdout = h.mustRun(t, "suggests", "--optional")
	if !strings.Contains(stdout, "  • sodium (optional iris ^1.8)\n  • sodium (optional modmenu)\n") {
		t.Fatalf("suggests --optional text:\n%s", stdout)
	}
}

func TestSuggestsRecognisesAModLockedUnderItsSlug(t *testing.T) {
	h := newHarness(t)
	h.jars["sodium"] = makeJar(t, "sodium_fabric", h.jars["sodium"].filename, "client")
	h.jars["fabric-api"] = makeJarWith(t, "fabric-api", h.jars["fabric-api"].filename, "*", `"depends":{"fabricloader":">=0.17"},"suggests":{"sodium":"*","indium":"*"}`)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")

	stdout := h.mustRun(t, "add", "sodium", "--as", "speed")
	if strings.Contains(stdout, "suggests sodium") || !strings.Contains(stdout, "• fabric-api (suggests indium, not installed)") {
		t.Fatalf("add should treat a slug match as installed:\n%s", stdout)
	}
	if got := h.readLock(t).Mods["speed"].Slug; got != "sodium" {
		t.Fatalf("the lock should record the slug, got %q", got)
	}

	stdout = h.mustRun(t, "suggests")
	if want := "  • fabric-api (suggests indium)\n  • fabric-api (suggests sodium, installed as speed)\n"; stdout != want {
		t.Fatalf("suggests text:\n%s", stdout)
	}
}

func TestAddRecordsTheSlugOfAModAlreadyLocked(t *testing.T) {
	h := newHarness(t)
	h.jars["sodium"] = makeJar(t, "sodium_fabric", h.jars["sodium"].filename, "client")
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	h.mustRun(t, "add", "sodium", "--as", "speed")
	l := h.readLock(t)
	m := l.Mods["speed"]
	m.Slug = ""
	l.Mods["speed"] = m
	if err := l.Save(filepath.Join(h.dir, "shulker.lock")); err != nil {
		t.Fatal(err)
	}

	h.mustRun(t, "add", "sodium", "--as", "speed")
	if got := h.readLock(t).Mods["speed"].Slug; got != "sodium" {
		t.Fatalf("re-adding should record the slug, got %q", got)
	}
}
