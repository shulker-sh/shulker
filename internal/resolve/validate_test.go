package resolve

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/jarmeta"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
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
		{"4.0.3.4", ">=4.0.3.2", true},
		{"4.0.3.4", ">=4.0.3.5", false},
		{"3.12.3.3+fabric-1.20.1", ">=3.12.2", true},
		{"custom", "custom", true},
		{"custom", ">=1.0", false},
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

func TestSatisfiesMinecraftAsFabricSeesIt(t *testing.T) {
	for _, c := range []struct {
		version, declared string
		want              bool
	}{
		{"24w33a", ">=1.21.2-", true},
		{"24w33a", ">=1.21.2", false},
		{"24w33a", ">1.21.1", true},
		{"1.21-pre1", ">=1.21-beta.1", true},
		{"1.20.1", "~1.20", true},
	} {
		got, err := satisfies(&jarmeta.Info{}, "minecraft", c.version, c.declared)
		if err != nil || got != c.want {
			t.Errorf("satisfies(minecraft %q, %q) = %v, %v, want %v", c.version, c.declared, got, err, c.want)
		}
	}
	if got, _ := satisfies(&jarmeta.Info{}, "some-mod", "24w33a", ">=1.21.2-"); got {
		t.Error("a mod versioned like a weekly snapshot is matched as the game")
	}
}

func TestSatisfiesUnparsable(t *testing.T) {
	for _, c := range [][2]string{{"1.0.0", ">=1.x"}, {"1.0.0", "<custom"}, {"1.0.0", "<1.0 || >x"}} {
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

func zipFiles(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func fabricJar(t *testing.T, meta string, nested map[string][]byte) []byte {
	files := map[string]string{"fabric.mod.json": meta}
	for name, jar := range nested {
		files[name] = string(jar)
	}
	return zipFiles(t, files)
}

func lockJars(t *testing.T, loader string, jars map[string][]byte) *Resolver {
	t.Helper()
	c := &cache.Cache{Dir: t.TempDir()}
	l := &lock.Lock{Minecraft: "26.2", Loader: lock.Loader{Type: loader, Version: "0.17.3"}, Mods: map[string]lock.Mod{}}
	for id, jar := range jars {
		sha, err := c.Put(bytes.NewReader(jar))
		if err != nil {
			t.Fatal(err)
		}
		l.Mods[id] = lock.Mod{Sha512: sha}
	}
	return &Resolver{Manifest: &manifest.Manifest{}, Lock: l, Cache: c}
}

func TestValidatePicksAmongNestedCopies(t *testing.T) {
	renderer := func(v string) []byte {
		return fabricJar(t, `{"id":"fabric-renderer-api-v1","version":"`+v+`"}`, nil)
	}
	oldAPI := fabricJar(t, `{"id":"fabric-api","version":"0.83.0+1.20.1","provides":["fabric"],"jars":[{"file":"META-INF/jars/r.jar"}]}`,
		map[string][]byte{"META-INF/jars/r.jar": renderer("3.0.1+b3afc78b82")})
	cloth := func(v string) []byte { return fabricJar(t, `{"id":"cloth-config2","version":"`+v+`"}`, nil) }
	bundling := func(id string, jar []byte) []byte {
		return fabricJar(t, `{"id":"`+id+`","version":"1.0.0","jars":[{"file":"META-INF/jars/n.jar"}]}`, map[string][]byte{"META-INF/jars/n.jar": jar})
	}
	lib := func(v string) []byte { return fabricJar(t, `{"id":"lib","version":"`+v+`"}`, nil) }
	r := lockJars(t, "fabric", map[string][]byte{
		"coroutil": bundling("coroutil", oldAPI),
		"fabric-api": fabricJar(t, `{"id":"fabric-api","version":"0.92.7+1.20.1","provides":["fabric"],"jars":[{"file":"META-INF/jars/r.jar"}]}`,
			map[string][]byte{"META-INF/jars/r.jar": renderer("3.2.1+aaaaaaaa")}),
		"accessories":     fabricJar(t, `{"id":"accessories","version":"1.0.0","depends":{"fabric":">=0.92.0"}}`, nil),
		"indium":          fabricJar(t, `{"id":"indium","version":"1.0.36","depends":{"fabric-renderer-api-v1":">=3.2.0"}}`, nil),
		"boatiview":       bundling("boatiview", cloth("11.0.99")),
		"quartzelv":       bundling("quartzelv", cloth("13.0.121")),
		"cloth-config2":   cloth("11.1.136"),
		"yungsmenutweaks": fabricJar(t, `{"id":"yungsmenutweaks","version":"1.0.2","depends":{"cloth-config2":">=11.1.106 <12"}}`, nil),
		"wantsnew":        fabricJar(t, `{"id":"wantsnew","version":"1.0.0","depends":{"cloth-config2":">=14"}}`, nil),
		"newlib":          bundling("newlib", lib("2.0.0")),
		"oldlib":          bundling("oldlib", lib("1.5.0")),
		"wantsold":        fabricJar(t, `{"id":"wantsold","version":"1.0.0","depends":{"lib":"<2"}}`, nil),
	})
	v, err := r.Validate()
	if err != nil {
		t.Fatal(err)
	}
	want := []Problem{{Rule: "depends", Mod: "wantsnew", ModVersion: "1.0.0", On: "cloth-config2", Declared: ">=14", Found: "11.1.136"}}
	if !reflect.DeepEqual(v.Problems, want) {
		t.Fatalf("problems %+v, want %+v", v.Problems, want)
	}
}

func TestValidatePrefersNewestFourPartCopy(t *testing.T) {
	lib := func(v string) []byte { return fabricJar(t, `{"id":"lib","version":"`+v+`"}`, nil) }
	bundling := func(id string, jar []byte) []byte {
		return fabricJar(t, `{"id":"`+id+`","version":"1.0.0","jars":[{"file":"META-INF/jars/n.jar"}]}`, map[string][]byte{"META-INF/jars/n.jar": jar})
	}
	r := lockJars(t, "fabric", map[string][]byte{
		"a":     bundling("a", lib("9.0.0.1")),
		"b":     bundling("b", lib("10.0.0.1")),
		"wants": fabricJar(t, `{"id":"wants","version":"1.0.0","depends":{"lib":">=11"}}`, nil),
	})
	v, err := r.Validate()
	if err != nil {
		t.Fatal(err)
	}
	want := []Problem{{Rule: "depends", Mod: "wants", ModVersion: "1.0.0", On: "lib", Declared: ">=11", Found: "10.0.0.1"}}
	if !reflect.DeepEqual(v.Problems, want) || len(v.Warnings) > 0 {
		t.Fatalf("problems %+v, warnings %v, want %+v", v.Problems, v.Warnings, want)
	}
}

func TestValidateQuiltKeepsTopLevelJar(t *testing.T) {
	quilt := func(id, v, extra string) string {
		return `{"schema_version":1,"quilt_loader":{"id":"` + id + `","version":"` + v + `"` + extra + `}}`
	}
	r := lockJars(t, "quilt", map[string][]byte{
		"lib": zipBytes(t, "quilt.mod.json", quilt("lib", "2.0.0", "")),
		"bundler": zipFiles(t, map[string]string{
			"quilt.mod.json":        quilt("bundler", "1.0.0", `,"jars":["META-INF/jars/lib.jar"]`),
			"META-INF/jars/lib.jar": string(zipBytes(t, "quilt.mod.json", quilt("lib", "1.5.0", ""))),
		}),
		"wantsold": zipBytes(t, "quilt.mod.json", quilt("wantsold", "1.0.0", `,"depends":[{"id":"lib","versions":"<2"}]`)),
	})
	v, err := r.Validate()
	if err != nil {
		t.Fatal(err)
	}
	want := []Problem{{Rule: "depends", Mod: "wantsold", ModVersion: "1.0.0", On: "lib", Declared: "<2", Found: "2.0.0"}}
	if !reflect.DeepEqual(v.Problems, want) {
		t.Fatalf("problems %+v, want %+v", v.Problems, want)
	}
}

func TestValidateAppliesFabricDependencyOverrides(t *testing.T) {
	r := lockJars(t, "fabric", map[string][]byte{
		"bwg":     fabricJar(t, `{"id":"bwg","version":"1.6.6","depends":{"terrablender":">=3.0.1.7"}}`, nil),
		"climate": fabricJar(t, `{"id":"climate","version":"20.1.0","depends":{"terrablender":"*"}}`, nil),
		"slabs":   fabricJar(t, `{"id":"slabs","version":"1.0.0"}`, nil),
	})
	r.Dir = t.TempDir()
	write := func(rel, data string) {
		path := filepath.Join(r.Dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("overrides/config/fabric_loader_dependencies.json", `{"version":1,"overrides":{
		"bwg":{"-depends":{"terrablender":"IGNORED"}},
		"climate":{"-depends":{"terrablender":"IGNORED"}},
		"slabs":{"+depends":{"balm":"*"}}}}`)
	write("server-overrides/config/fabric_loader_dependencies.json", `{"version":1,"overrides":{
		"bwg":{"-depends":{"terrablender":"IGNORED"}}}}`)
	v, err := r.Validate()
	if err != nil {
		t.Fatal(err)
	}
	want := []Problem{
		{Rule: "depends", Mod: "climate", ModVersion: "20.1.0", On: "terrablender", Declared: "*"},
		{Rule: "depends", Mod: "slabs", ModVersion: "1.0.0", On: "balm", Declared: "*"},
	}
	if !reflect.DeepEqual(v.Problems, want) {
		t.Fatalf("problems %+v, want %+v", v.Problems, want)
	}
	if v, err = r.Validate("client"); err != nil {
		t.Fatal(err)
	}
	want = []Problem{{Rule: "depends", Mod: "slabs", ModVersion: "1.0.0", On: "balm", Declared: "*"}}
	if !reflect.DeepEqual(v.Problems, want) {
		t.Fatalf("client problems %+v, want %+v", v.Problems, want)
	}
	write("overrides/config/fabric_loader_dependencies.json", `{"overrides":{}}`)
	if _, err := r.Validate("client"); out.CodeOf(err) != "dependency-overrides-invalid" {
		t.Fatalf("err %v, want dependency-overrides-invalid", err)
	}
}
