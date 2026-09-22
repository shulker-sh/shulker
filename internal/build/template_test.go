package build

import (
	"errors"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func TestRenderUnsetVariable(t *testing.T) {
	vars := map[string]string{"motd": "hi", "pack.name": "pack", "minecraft.version": "26.2"}
	for _, tc := range []struct {
		name, pack, data, help string
	}{
		{"a pack's own variable says how to set it", "", "${greeting}", "run `shulker set variables.greeting <value>`"},
		{"a pulled pack's plain variable is set in the project too", "base", "${greeting}", "run `shulker set variables.greeting <value>`"},
		{"the project's version", "", "${pack.version}", "run `shulker set version <version>`"},
		{"a pulled pack's version is its own", "base", "${pack.version}", `set "version" in base's shulker.json`},
		{"the loader comes from the lock", "", "${loader.type}", "the lock has no loader: set \"loader\" in shulker.json, then run `shulker lock`"},
		{"a misspelt built-in has no help", "", "${pack.verison}", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := render("overrides/a.txt.tmpl", tc.pack, []byte("x\n"+tc.data), vars)
			var e *out.Error
			if !errors.As(err, &e) || e.Code != "unset-variable" {
				t.Fatalf("got %v", err)
			}
			if e.Help != tc.help {
				t.Errorf("help = %q, want %q", e.Help, tc.help)
			}
			if want := "overrides/a.txt.tmpl:2: variable " + tc.data + " is not set"; e.Message != want {
				t.Errorf("message = %q, want %q", e.Message, want)
			}
		})
	}

	_, err := render("a.tmpl", "", []byte("${pack.nme}"), vars)
	var e *out.Error
	errors.As(err, &e)
	if e.Given != "pack.nme" || !slices.Equal(e.Candidates, []string{"minecraft.version", "motd", "pack.name"}) {
		t.Errorf("a near miss offers the set variables as candidates: %q %q", e.Given, e.Candidates)
	}
	_, err = render("a.tmpl", "", []byte("${pack.version}"), vars)
	if errors.As(err, &e); len(e.Candidates) != 0 {
		t.Errorf("a variable that is simply missing offers no candidates: %q", e.Candidates)
	}
}
