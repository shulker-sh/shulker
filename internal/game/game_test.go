package game

import (
	"archive/zip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/out"
)

var linux64 = Platform{OS: "linux", Arch: "x86_64"}

func parse(t *testing.T, doc string) Version {
	t.Helper()
	v, err := ParseVersion([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func names(libs []Library) []string {
	out := make([]string, len(libs))
	for i, l := range libs {
		out[i] = l.Name
	}
	return out
}

func TestMergePutsTheLoaderFirstAndItsArgumentsLast(t *testing.T) {
	vanilla := parse(t, `{
		"id": "26.2", "type": "release", "mainClass": "net.minecraft.client.main.Main",
		"libraries": [{"name": "com.mojang:brigadier:1.3.10"}],
		"arguments": {"game": ["--username", "${auth_player_name}"], "jvm": ["-Dvanilla=1"]},
		"downloads": {"client": {"url": "https://example.test/client.jar", "sha1": "abc"}},
		"assetIndex": {"id": "26", "url": "https://example.test/26.json"}
	}`)
	fabric := parse(t, `{
		"id": "fabric-loader-0.17.3-26.2", "inheritsFrom": "26.2",
		"mainClass": "net.fabricmc.loader.impl.launch.knot.KnotClient",
		"libraries": [{"name": "net.fabricmc:fabric-loader:0.17.3", "url": "https://maven.test/"}],
		"arguments": {"jvm": ["-DFabricMcEmu=net.minecraft.client.main.Main"]}
	}`)

	merged := Merge(vanilla, fabric)

	if merged.ID != "fabric-loader-0.17.3-26.2" || merged.InheritsFrom != "" {
		t.Fatalf("id %q inherits %q", merged.ID, merged.InheritsFrom)
	}
	if merged.MainClass != "net.fabricmc.loader.impl.launch.knot.KnotClient" {
		t.Fatalf("main class %q", merged.MainClass)
	}
	if merged.Type != "release" || merged.AssetIndex.ID != "26" || merged.Downloads["client"].Sha1 != "abc" {
		t.Fatalf("vanilla fields lost: %+v", merged)
	}
	if got := names(merged.Libraries); !slices.Equal(got, []string{"net.fabricmc:fabric-loader:0.17.3", "com.mojang:brigadier:1.3.10"}) {
		t.Fatalf("libraries %q", got)
	}
	jvm, game := Args(merged, linux64, nil, map[string]string{"auth_player_name": "Notch"})
	if !slices.Equal(jvm, []string{"-Dvanilla=1", "-DFabricMcEmu=net.minecraft.client.main.Main"}) {
		t.Fatalf("jvm %q", jvm)
	}
	if !slices.Equal(game, []string{"--username", "Notch"}) {
		t.Fatalf("game %q", game)
	}
}

func TestALoaderSharesTheClientJarOfTheVersionItInheritsFrom(t *testing.T) {
	s := Store{Root: t.TempDir()}
	write(t, s, `{"id": "26.2", "downloads": {"client": {"url": "https://example.test/client.jar", "size": 9}}}`)
	write(t, s, `{"id": "fabric", "inheritsFrom": "26.2"}`)

	v, err := s.Resolve("fabric")
	if err != nil {
		t.Fatal(err)
	}
	a, err := Assemble(v, linux64, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.Client.Path != "versions/26.2/26.2.jar" {
		t.Fatalf("a loader should not file a second copy of the client jar: %s", a.Client.Path)
	}
}

func TestMergeLetsMinecraftArgumentsReplaceTheParentsGameArguments(t *testing.T) {
	vanilla := parse(t, `{"id": "1.12.2", "minecraftArguments": "--username ${auth_player_name} --version ${version_name}"}`)
	forge := parse(t, `{"id": "1.12.2-forge", "inheritsFrom": "1.12.2", "minecraftArguments": "--username ${auth_player_name} --version ${version_name} --tweakClass FML"}`)

	_, game := Args(Merge(vanilla, forge), linux64, nil, map[string]string{"auth_player_name": "Notch", "version_name": "1.12.2-forge"})

	if !slices.Equal(game, []string{"--username", "Notch", "--version", "1.12.2-forge", "--tweakClass", "FML"}) {
		t.Fatalf("game %q", game)
	}
}

func TestMergeKeepsTheParentsLegacyArgumentsUnderAChildsList(t *testing.T) {
	vanilla := parse(t, `{"id": "1.12.2", "minecraftArguments": "--username ${auth_player_name}"}`)
	child := parse(t, `{"id": "1.12.2-fabric", "inheritsFrom": "1.12.2", "arguments": {"game": ["--tweakClass", "Knot"]}}`)

	_, game := Args(Merge(vanilla, child), linux64, nil, map[string]string{"auth_player_name": "Notch"})

	if !slices.Equal(game, []string{"--username", "Notch", "--tweakClass", "Knot"}) {
		t.Fatalf("game %q", game)
	}
}

func TestRulesTakeTheLastMatchAndRefuseWhatIsNeverMentioned(t *testing.T) {
	rules := []Rule{
		{Action: "allow"},
		{Action: "disallow", OS: &OSRule{Name: "osx"}},
	}
	if !Allows(rules, linux64, nil) {
		t.Fatal("linux should be allowed")
	}
	if Allows(rules, Platform{OS: "osx", Arch: "arm64"}, nil) {
		t.Fatal("osx should be refused")
	}
	only := []Rule{{Action: "allow", OS: &OSRule{Name: "windows", Arch: "x86"}}}
	if Allows(only, Platform{OS: "windows", Arch: "x86_64"}, nil) {
		t.Fatal("a 64-bit host matched a 32-bit rule")
	}
	if !Allows(only, Platform{OS: "windows", Arch: "x86"}, nil) {
		t.Fatal("a 32-bit host was refused")
	}
	if Allows(nil, linux64, nil) != true {
		t.Fatal("no rules means allowed")
	}
}

func TestRuleNamingAnOSVersionNeverMatchesWhenTheVersionIsUnknown(t *testing.T) {
	rules := []Rule{{Action: "allow", OS: &OSRule{Name: "osx", Version: `^10\.5\.\d$`}}}
	if Allows(rules, Platform{OS: "osx", Arch: "x86_64"}, nil) {
		t.Fatal("matched without an os version")
	}
	if !Allows(rules, Platform{OS: "osx", Arch: "x86_64", Version: "10.5.8"}, nil) {
		t.Fatal("did not match the os version it names")
	}
}

func TestFeatureRulesGateAnArgument(t *testing.T) {
	v := parse(t, `{"id": "26.2", "arguments": {"game": ["--username", {"rules": [{"action": "allow", "features": {"is_demo_user": true}}], "value": "--demo"}]}}`)

	_, off := Args(v, linux64, nil, nil)
	if !slices.Equal(off, []string{"--username"}) {
		t.Fatalf("off %q", off)
	}
	_, on := Args(v, linux64, map[string]bool{"is_demo_user": true}, nil)
	if !slices.Equal(on, []string{"--username", "--demo"}) {
		t.Fatalf("on %q", on)
	}
}

func TestAVersionWithNoJVMArgumentsGetsTheLegacyPair(t *testing.T) {
	v := parse(t, `{"id": "1.12.2", "minecraftArguments": "--version ${version_name}"}`)

	jvm, _ := Args(v, linux64, nil, map[string]string{"natives_directory": "/n", "classpath": "/a.jar:/b.jar"})

	if !slices.Equal(jvm, []string{"-Djava.library.path=/n", "-cp", "/a.jar:/b.jar"}) {
		t.Fatalf("jvm %q", jvm)
	}
}

func TestAssembleKeepsClasspathOrderAndSplitsOffNatives(t *testing.T) {
	v := parse(t, `{
		"id": "1.12.2",
		"downloads": {"client": {"url": "https://example.test/client.jar", "sha1": "cc", "size": 9}},
		"assetIndex": {"id": "1.12", "url": "https://example.test/1.12.json", "sha1": "ai"},
		"libraries": [
			{"name": "net.fabricmc:fabric-loader:0.17.3", "url": "https://maven.test/"},
			{"name": "com.mojang:brigadier:1.3.10", "downloads": {"artifact": {"path": "com/mojang/brigadier/1.3.10/brigadier-1.3.10.jar", "url": "https://libs.test/brigadier.jar", "size": 4}}},
			{"name": "net.fabricmc:fabric-loader:0.17.3", "url": "https://maven.test/"},
			{"name": "org.lwjgl:lwjgl-platform:2.9.4", "natives": {"linux": "natives-linux", "windows": "natives-windows-${arch}"}, "extract": {"exclude": ["META-INF/"]}},
			{"name": "org.macos:only:1.0", "rules": [{"action": "allow", "os": {"name": "osx"}}]}
		]
	}`)

	a, err := Assemble(v, linux64, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := Store{Root: "/store"}
	want := []string{
		filepath.Join("/store", "libraries", "net", "fabricmc", "fabric-loader", "0.17.3", "fabric-loader-0.17.3.jar"),
		filepath.Join("/store", "libraries", "com", "mojang", "brigadier", "1.3.10", "brigadier-1.3.10.jar"),
		filepath.Join("/store", "versions", "1.12.2", "1.12.2.jar"),
	}
	if got := a.Classpath(s); !slices.Equal(got, want) {
		t.Fatalf("classpath %q", got)
	}
	if len(a.Natives) != 1 || a.Natives[0].Path != "libraries/org/lwjgl/lwjgl-platform/2.9.4/lwjgl-platform-2.9.4-natives-linux.jar" {
		t.Fatalf("natives %+v", a.Natives)
	}
	if got := a.Excludes[a.Natives[0].Path]; !slices.Equal(got, []string{"META-INF/"}) {
		t.Fatalf("excludes %q", got)
	}
	if a.AssetIndex.Path != "assets/indexes/1.12.json" || a.Client.Path != "versions/1.12.2/1.12.2.jar" {
		t.Fatalf("assembly %+v", a)
	}
	if got := a.ClasspathSize(s); got != 13 {
		t.Fatalf("classpath size %d", got)
	}
}

func TestAssembleFillsTheArchInANativesClassifier(t *testing.T) {
	v := parse(t, `{
		"id": "1.12.2",
		"downloads": {"client": {"url": "https://example.test/client.jar"}},
		"libraries": [{"name": "org.lwjgl:lwjgl-platform:2.9.4", "natives": {"windows": "natives-windows-${arch}"}}]
	}`)

	for _, tc := range []struct{ arch, want string }{{"x86", "32"}, {"x86_64", "64"}} {
		a, err := Assemble(v, Platform{OS: "windows", Arch: tc.arch}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(a.Natives[0].Path, "lwjgl-platform-2.9.4-natives-windows-"+tc.want+".jar") {
			t.Fatalf("%s: %s", tc.arch, a.Natives[0].Path)
		}
	}
}

func TestANativeWithNoClassifierForThePlatformIsLeftOut(t *testing.T) {
	v := parse(t, `{
		"id": "1.12.2",
		"downloads": {"client": {"url": "https://example.test/client.jar"}},
		"libraries": [{"name": "org.lwjgl:lwjgl-platform:2.9.4", "natives": {"windows": "natives-windows"}}]
	}`)

	a, err := Assemble(v, Platform{OS: "osx", Arch: "arm64"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Natives) != 0 || len(a.Libraries) != 0 {
		t.Fatalf("assembly %+v", a)
	}
}

func TestAssembleFailsWithoutAClientDownload(t *testing.T) {
	if _, err := Assemble(parse(t, `{"id": "26.2"}`), linux64, nil); out.CodeOf(err) != "store-incomplete" {
		t.Fatalf("err %v", err)
	}
}

func TestResolveWalksTheInheritsFromChain(t *testing.T) {
	s := Store{Root: t.TempDir()}
	write(t, s, `{"id": "26.2", "type": "release", "mainClass": "Vanilla", "libraries": [{"name": "com.mojang:brigadier:1.3.10"}]}`)
	write(t, s, `{"id": "fabric", "inheritsFrom": "26.2", "mainClass": "Knot", "libraries": [{"name": "net.fabricmc:fabric-loader:0.17.3"}]}`)

	v, err := s.Resolve("fabric")
	if err != nil {
		t.Fatal(err)
	}
	if v.MainClass != "Knot" || v.Type != "release" {
		t.Fatalf("version %+v", v)
	}
	if got := names(v.Libraries); !slices.Equal(got, []string{"net.fabricmc:fabric-loader:0.17.3", "com.mojang:brigadier:1.3.10"}) {
		t.Fatalf("libraries %q", got)
	}
}

func TestResolveRefusesAVersionThatInheritsFromItself(t *testing.T) {
	s := Store{Root: t.TempDir()}
	write(t, s, `{"id": "loop", "inheritsFrom": "loop"}`)

	if _, err := s.Resolve("loop"); out.CodeOf(err) != "store-incomplete" {
		t.Fatalf("err %v", err)
	}
}

func TestResolveNamesTheVersionTheStoreIsMissing(t *testing.T) {
	s := Store{Root: t.TempDir()}
	write(t, s, `{"id": "fabric", "inheritsFrom": "26.2"}`)

	_, err := s.Resolve("fabric")

	if out.CodeOf(err) != "store-incomplete" || !strings.Contains(out.AsError(err).Message, "26.2") {
		t.Fatalf("err %v", err)
	}
}

func write(t *testing.T, s Store, doc string) {
	t.Helper()
	if _, err := s.SaveVersion([]byte(doc)); err != nil {
		t.Fatal(err)
	}
}

func TestHasTakesTheSizeAndFallsBackToTheHash(t *testing.T) {
	s := Store{Root: t.TempDir()}
	f := File{Path: "libraries/a.jar", Sha1: "a9993e364706816aba3e25717850c26c9cd0d89d", Size: 3}
	if s.Has(f) {
		t.Fatal("an absent file is not in the store")
	}
	if err := os.MkdirAll(filepath.Join(s.Root, "libraries"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := fsutil.Write(s.Local(f), []byte("abc")); err != nil {
		t.Fatal(err)
	}
	if !s.Has(f) {
		t.Fatal("the file is there at the size the version json gives")
	}
	if s.Has(File{Path: f.Path, Size: 4}) {
		t.Fatal("a different size is a different file")
	}
	if !s.Has(File{Path: f.Path, Sha1: f.Sha1}) {
		t.Fatal("with no size the hash should answer")
	}
	if s.Has(File{Path: f.Path, Sha1: strings.Repeat("0", 40)}) {
		t.Fatal("a different hash is not the same file")
	}
}

func TestAssetFilesAreStoredByHashAndCountedOnce(t *testing.T) {
	s := Store{Root: t.TempDir(), Resources: "https://res.test"}
	if err := os.MkdirAll(filepath.Join(s.Root, "assets", "indexes"), 0o755); err != nil {
		t.Fatal(err)
	}
	index := `{"objects": {
		"icons/icon_16x16.png": {"hash": "bdf48ef6b5d0d23bbb02e17d04865216179f510a", "size": 3665},
		"icons/duplicate.png": {"hash": "bdf48ef6b5d0d23bbb02e17d04865216179f510a", "size": 3665},
		"minecraft/sounds/ambient.ogg": {"hash": "ffd1ae5e0a4bd5a1e7e1e8e79b5cb0f1e2a3b4c5", "size": 12}
	}}`
	if err := fsutil.Write(s.AssetIndex("26"), []byte(index)); err != nil {
		t.Fatal(err)
	}

	files, err := s.AssetFiles("26")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files %+v", files)
	}
	if files[0].Path != "assets/objects/bd/bdf48ef6b5d0d23bbb02e17d04865216179f510a" {
		t.Fatalf("path %q", files[0].Path)
	}
	if files[0].URL != "https://res.test/bd/bdf48ef6b5d0d23bbb02e17d04865216179f510a" {
		t.Fatalf("url %q", files[0].URL)
	}
}

func TestExtractNativesHonoursExcludesAndRenamesJnilibOnMacOS(t *testing.T) {
	s := Store{Root: t.TempDir()}
	jar := File{Path: "libraries/natives.jar"}
	writeJar(t, s.Local(jar), map[string]string{
		"liblwjgl.jnilib":        "binary",
		"META-INF/MANIFEST.MF":   "manifest",
		"../escape.so":           "nope",
		"subdir/libopenal.dylib": "more binary",
	})
	a := Assembly{Natives: []File{jar}, Excludes: map[string][]string{jar.Path: {"META-INF/"}}}
	dir := filepath.Join(t.TempDir(), "natives")

	if err := a.ExtractNatives(s, dir, Platform{OS: "osx", Arch: "arm64"}); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"liblwjgl.dylib", filepath.Join("subdir", "libopenal.dylib")} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "META-INF", "MANIFEST.MF")); err == nil {
		t.Fatal("an excluded entry was unpacked")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape.so")); err == nil {
		t.Fatal("an entry escaped the natives directory")
	}
}

func TestExtractNativesKeepsJnilibOffMacOS(t *testing.T) {
	s := Store{Root: t.TempDir()}
	jar := File{Path: "libraries/natives.jar"}
	writeJar(t, s.Local(jar), map[string]string{"liblwjgl.jnilib": "binary"})
	dir := filepath.Join(t.TempDir(), "natives")

	if err := (Assembly{Natives: []File{jar}}).ExtractNatives(s, dir, linux64); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "liblwjgl.jnilib")); err != nil {
		t.Fatal(err)
	}
}

func writeJar(t *testing.T, path string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for name, body := range files {
		entry, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestVarsDescribeTheLaunchWithoutTheAccount(t *testing.T) {
	s := Store{Root: filepath.Join("/store")}
	a := Assembly{
		Version:   Version{ID: "26.2", Type: "release", AssetIndex: &AssetIndexRef{ID: "26"}},
		Client:    File{Path: "versions/26.2/26.2.jar"},
		Libraries: []File{{Path: "libraries/a.jar"}},
	}

	vars := a.Vars(s, "shulker", "0.1.0", "/games/pack", "/games/pack/natives")

	if vars["assets_index_name"] != "26" || vars["version_type"] != "release" {
		t.Fatalf("vars %v", vars)
	}
	if !strings.Contains(vars["classpath"], filepath.Join("/store", "versions", "26.2", "26.2.jar")) {
		t.Fatalf("classpath %q", vars["classpath"])
	}
	for _, name := range []string{"auth_access_token", "auth_player_name", "auth_uuid"} {
		if _, ok := vars[name]; ok {
			t.Fatalf("%s belongs to the account, not the store", name)
		}
	}
}

func TestEnsureProfilesWritesTheStubTheInstallersNeed(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "game")}
	if err := s.EnsureProfiles(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(s.Root, "launcher_profiles.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"profiles"`) {
		t.Fatalf("stub %s", data)
	}
}
