package build

import (
	"errors"
	"maps"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

func TestTemplateVarsBuiltins(t *testing.T) {
	m := &manifest.Manifest{Name: "fo", Version: "7.0.0", Client: &manifest.Client{Name: "Fabulously Optimized"}}
	l := &lock.Lock{Minecraft: "26.2", DataVersion: 4903, Java: lock.Java{Major: 25}, Loader: lock.Loader{Type: "fabric", Version: "0.17.3"}}
	want := map[string]string{
		"project.name": "fo", "project.displayName": "Fabulously Optimized", "project.version": "7.0.0",
		"minecraft.version": "26.2", "minecraft.dataVersion": "4903", "java.major": "25",
		"loader.type": "fabric", "loader.version": "0.17.3",
	}
	if got := templateVars(m, l, "client"); !maps.Equal(got, want) {
		t.Errorf("client: got %v", got)
	}
	if got := templateVars(m, l, "server")["project.displayName"]; got != "fo" {
		t.Errorf("a side without a name shows the project's: %q", got)
	}
	got := templateVars(m, &lock.Lock{Minecraft: "1.7.10"}, "server")
	for _, unset := range []string{"minecraft.dataVersion", "java.major", "loader.type"} {
		if v, ok := got[unset]; ok {
			t.Errorf("%s = %q, want unset", unset, v)
		}
	}
	pulled := pulledTemplateVars(&manifest.Manifest{Name: "base"}, l, "client", templateVars(m, l, "client"))
	if pulled["project.name"] != "base" || pulled["project.displayName"] != "base" || pulled["project.version"] != "" || pulled["minecraft.dataVersion"] != "4903" {
		t.Errorf("a pulled pack's project.* are its own: %v", pulled)
	}
}

func TestRenderUnsetVariable(t *testing.T) {
	vars := map[string]string{"motd": "hi", "project.name": "pack", "minecraft.version": "26.2"}
	for _, tc := range []struct {
		name, pack, data, help string
	}{
		{"a pack's own variable says how to set it", "", "${greeting}", "run `shulker set variables.greeting <value>`"},
		{"a pulled pack's plain variable is set in the project too", "base", "${greeting}", "run `shulker set variables.greeting <value>`"},
		{"the project's version", "", "${project.version}", "run `shulker set version <version>`"},
		{"a pulled pack's version is its own", "base", "${project.version}", `set "version" in base's shulker.json`},
		{"the data version comes from the lock", "", "${minecraft.dataVersion}", "run `shulker lock`"},
		{"the Java version comes from the lock", "", "${java.major}", "run `shulker lock`"},
		{"the loader comes from the lock", "", "${loader.type}", "the lock has no loader: set \"loader\" in shulker.json, then run `shulker lock`"},
		{"a misspelt built-in has no help", "", "${project.verison}", ""},
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

	_, err := render("a.tmpl", "", []byte("${project.nme}"), vars)
	var e *out.Error
	errors.As(err, &e)
	if e.Given != "project.nme" || !slices.Equal(e.Candidates, []string{"minecraft.version", "motd", "project.name"}) {
		t.Errorf("a near miss offers the set variables as candidates: %q %q", e.Given, e.Candidates)
	}
	_, err = render("a.tmpl", "", []byte("${project.version}"), vars)
	if errors.As(err, &e); len(e.Candidates) != 0 {
		t.Errorf("a variable that is simply missing offers no candidates: %q", e.Candidates)
	}
}
