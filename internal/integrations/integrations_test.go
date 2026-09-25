package integrations

import (
	"maps"
	"slices"
	"testing"
)

func set(ids ...string) map[string]bool {
	s := map[string]bool{}
	for _, id := range ids {
		s[id] = true
	}
	return s
}

func TestMatch(t *testing.T) {
	cases := []struct {
		name      string
		placed    []string
		overrides map[string][]string
		want      []string
	}{
		{name: "built-in jar ids", placed: []string{"iris", "paxi", "sodium"}, want: []string{"iris", "paxi"}},
		{name: "nothing placed", want: nil},
		{name: "override adds a fork", placed: []string{"iris_fork"}, overrides: map[string][]string{"iris": {"iris", "iris_fork"}}, want: []string{"iris"}},
		{name: "override replaces built-in ids", placed: []string{"iris"}, overrides: map[string][]string{"iris": {"iris_fork"}}, want: nil},
		{name: "empty override turns it off", placed: []string{"paxi", "openloader"}, overrides: map[string][]string{"paxi": {}}, want: []string{"openloader"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := slices.Sorted(maps.Keys(Match(set(c.placed...), c.overrides)))
			if !slices.Equal(got, c.want) {
				t.Errorf("Match = %v, want %v", got, c.want)
			}
		})
	}
}

func TestIDsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, id := range IDs() {
		if seen[id] {
			t.Errorf("integration id %q is used twice", id)
		}
		seen[id] = true
	}
}

func shader(id string) Shader {
	for _, s := range Shaders {
		if s.ID == id {
			return s
		}
	}
	panic("no shader " + id)
}

func TestShaderLoads(t *testing.T) {
	iris := shader("iris")
	cases := []struct {
		name    string
		loaders []string
		present []string
		want    bool
	}{
		{name: "names it", loaders: []string{"iris", "oculus"}, present: []string{"iris"}, want: true},
		{name: "names none", present: []string{"iris"}, want: true},
		{name: "names another", loaders: []string{"canvas"}, present: []string{"iris"}, want: false},
		{name: "not present", loaders: []string{"iris"}, present: []string{"oculus"}, want: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := iris.Loads(c.loaders, set(c.present...)); got != c.want {
				t.Errorf("Loads = %v, want %v", got, c.want)
			}
		})
	}
}

func TestDatapackLoaderFolder(t *testing.T) {
	var openLoader DatapackLoader
	for _, l := range DatapackLoaders {
		if l.ID == "openloader" {
			openLoader = l
		}
	}
	cases := map[string]string{
		"1.20.1":  "config/openloader/data",
		"1.21":    "config/openloader/packs",
		"26.2":    "config/openloader/packs",
		"unknown": "config/openloader/packs",
	}
	for mc, want := range cases {
		if got := openLoader.Folder(mc); got != want {
			t.Errorf("Folder(%s) = %s, want %s", mc, got, want)
		}
	}
	if got := DatapackLoaders[0].Folder("1.20.1"); got != "config/paxi/datapacks" {
		t.Errorf("paxi Folder = %s", got)
	}
}

func TestShaderTags(t *testing.T) {
	cases := map[string][]string{
		"modrinth":   {"iris", "optifine", "canvas"},
		"curseforge": {"iris", "optifine"},
		"other":      nil,
	}
	for provider, want := range cases {
		if got := ShaderTags(provider); !slices.Equal(got, want) {
			t.Errorf("ShaderTags(%s) = %v, want %v", provider, got, want)
		}
	}
}

func TestShadersTagged(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		tags     []string
		want     []string
	}{
		{name: "modrinth iris", provider: "modrinth", tags: []string{"iris"}, want: []string{"iris"}},
		{name: "modrinth optifine", provider: "modrinth", tags: []string{"iris", "optifine"}, want: []string{"iris", "oculus"}},
		{name: "vanilla is no integration", provider: "modrinth", tags: []string{"vanilla", "canvas"}, want: []string{"canvas"}},
		{name: "curseforge optifine", provider: "curseforge", tags: []string{"26.2", "OptiFine"}, want: []string{"iris", "oculus"}},
		{name: "curseforge iris and optifine", provider: "curseforge", tags: []string{"Iris", "OptiFine"}, want: []string{"iris", "oculus"}},
		{name: "curseforge has no canvas tag", provider: "curseforge", tags: []string{"canvas"}},
		{name: "untagged", provider: "modrinth", tags: []string{"26.2"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ShadersTagged(c.provider, c.tags); !slices.Equal(got, c.want) {
				t.Errorf("ShadersTagged = %v, want %v", got, c.want)
			}
		})
	}
}
