package resolve

import (
	"bytes"
	"reflect"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
)

func TestSatisfies(t *testing.T) {
	cases := []struct {
		version, declared string
		want              bool
	}{
		{"0.17.3", ">=0.17", true},
		{"0.16.9", ">=0.17", false},
		{"0.18.0-beta.1", ">=0.17", true},
		{"26.2", "26.2", true},
		{"26.2", "*", true},
		{"26.3-pre-1", "*", true},
		{"26.2", "26.x", true},
		{"26.2", "~26.2-", true},
		{"26.3-pre-1", ">=26.2-", true},
		{"26.3-pre-1", ">=26.3", false},
		{"24w33a", ">=1.21.2-", true},
		{"24w33a", ">=1.21.2", false},
		{"24w33a", ">1.21.1", true},
		{"27.1", "26.x", false},
		{"0.5.2", "0.x", true},
		{"1.0.0", "0.x", false},
		{"26.2.1", "26.2.x", true},
		{"26.3", "26.2.x", false},
		{"0.130.0+26.2", ">=0.130.0", true},
		{"0.129.0+26.2", ">=0.130.0", false},
		{"25.0", ">=21", true},
		{"17.0", ">=21", false},
		{"1.2.3", "~1.2.0", true},
		{"1.3.0", "~1.2.0", false},
		{"1.9.0", "^1.2.0", true},
		{"2.0.0", "^1.2.0", false},
		{"1.0.0", "<0.9 || >=1.0", true},
		{"0.9.5", "<0.9 || >=1.0", false},
		{"1.0.0", ">=0.5 <2.0", true},
	}
	for _, c := range cases {
		got, err := fabricSatisfies(c.version, c.declared)
		if err != nil {
			t.Errorf("fabricSatisfies(%q, %q): %v", c.version, c.declared, err)
			continue
		}
		if got != c.want {
			t.Errorf("fabricSatisfies(%q, %q) = %v, want %v", c.version, c.declared, got, c.want)
		}
	}
}

func TestSatisfiesUnparsable(t *testing.T) {
	for _, c := range [][2]string{{"1.2.3.4", "*"}, {"1.0.0", ">=1.x"}, {"1.0.0", "1.2.3.4"}, {"26w10a", ">=26.2"}} {
		if _, err := fabricSatisfies(c[0], c[1]); err == nil {
			t.Errorf("fabricSatisfies(%q, %q) should not parse", c[0], c[1])
		}
	}
}

func TestValidateOptionalAndLoaderProvides(t *testing.T) {
	c := &cache.Cache{Dir: t.TempDir()}
	jars := map[string][2]string{
		"shiny":  {"quilt.mod.json", `{"schema_version":1,"quilt_loader":{"id":"shiny","version":"1.0.0","depends":[{"id":"sodium","versions":"^0.9","optional":true},{"id":"iris","versions":"^1.8","optional":true}]}}`},
		"sodium": {"fabric.mod.json", `{"id":"sodium","version":"1.0.0","depends":{"fabricloader":">=0.19"}}`},
	}
	l := &lock.Lock{Minecraft: "26.2", Loader: lock.Loader{Type: "quilt", Version: "0.31.0-beta.4", Provides: map[string]string{"fabricloader": "0.19.5"}}, Mods: map[string]lock.Mod{}}
	for id, jar := range jars {
		sha, err := c.Put(bytes.NewReader(zipBytes(t, jar[0], jar[1])))
		if err != nil {
			t.Fatal(err)
		}
		l.Mods[id] = lock.Mod{Sha512: sha}
	}
	r := &Resolver{Manifest: &manifest.Manifest{}, Lock: l, Cache: c}
	v, err := r.Validate()
	if err != nil {
		t.Fatal(err)
	}
	want := []Problem{{Rule: "depends", Mod: "shiny", ModVersion: "1.0.0", On: "sodium", Declared: "^0.9", Found: "1.0.0"}}
	if !reflect.DeepEqual(v.Problems, want) {
		t.Fatalf("problems %+v, want %+v", v.Problems, want)
	}
}

func TestValidateSkipsDependenciesNotDownloaded(t *testing.T) {
	c := &cache.Cache{Dir: t.TempDir()}
	sha, err := c.Put(bytes.NewReader(zipBytes(t, "fabric.mod.json", `{"id":"private-mod","version":"1.0.0","depends":{"fabric-api":"*","sodium":"*"}}`)))
	if err != nil {
		t.Fatal(err)
	}
	l := &lock.Lock{Minecraft: "26.2", Loader: lock.Loader{Type: "fabric", Version: "0.17.3"}, Mods: map[string]lock.Mod{
		"private-mod": {Sha512: sha},
		"fabric-api":  {Sha512: "0000"},
	}}
	v, err := (&Resolver{Manifest: &manifest.Manifest{}, Lock: l, Cache: c}).Validate()
	if err != nil {
		t.Fatal(err)
	}
	want := []Problem{{Rule: "depends", Mod: "private-mod", ModVersion: "1.0.0", On: "sodium", Declared: "*"}}
	if !reflect.DeepEqual(v.Problems, want) {
		t.Fatalf("problems %+v, want %+v", v.Problems, want)
	}
}

func TestValidateNeoForgeMavenRanges(t *testing.T) {
	c := &cache.Cache{Dir: t.TempDir()}
	jars := map[string]string{
		"sodium": "[[mods]]\nmodId=\"sodium\"\nversion=\"mc26.2-0.8.1\"\n" +
			"[[dependencies.sodium]]\nmodId=\"neoforge\"\ntype=\"required\"\nversionRange=\"[26.1.2.10-beta,)\"\n" +
			"[[dependencies.sodium]]\nmodId=\"minecraft\"\ntype=\"required\"\nversionRange=\"[26.2,26.3)\"\n",
		"iris": "[[mods]]\nmodId=\"iris\"\nversion=\"1.9.0\"\n" +
			"[[dependencies.iris]]\nmodId=\"sodium\"\ntype=\"required\"\nversionRange=\"[mc26.2-0.9,)\"\n" +
			"[[dependencies.iris]]\nmodId=\"neoforge\"\ntype=\"incompatible\"\nversionRange=\"(,26.2.0.50]\"\n",
	}
	l := &lock.Lock{Minecraft: "26.2", Loader: lock.Loader{Type: "neoforge", Version: "26.2.0.87"}, Mods: map[string]lock.Mod{}}
	for id, toml := range jars {
		sha, err := c.Put(bytes.NewReader(zipBytes(t, "META-INF/neoforge.mods.toml", toml)))
		if err != nil {
			t.Fatal(err)
		}
		l.Mods[id] = lock.Mod{Sha512: sha}
	}
	r := &Resolver{Manifest: &manifest.Manifest{}, Lock: l, Cache: c}
	v, err := r.Validate()
	if err != nil {
		t.Fatal(err)
	}
	want := []Problem{{Rule: "depends", Mod: "iris", ModVersion: "1.9.0", On: "sodium", Declared: "[mc26.2-0.9,)", Found: "mc26.2-0.8.1"}}
	if !reflect.DeepEqual(v.Problems, want) || len(v.Warnings) > 0 {
		t.Fatalf("problems %+v, warnings %v, want %+v", v.Problems, v.Warnings, want)
	}
}
