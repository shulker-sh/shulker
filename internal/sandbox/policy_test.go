package sandbox

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// layout is a fake home holding a runtime, a store, a saves root and an instance, with every path
// resolved the way Derive resolves it.
type layout struct {
	home, java, lib, client, natives, system, assets, gameDir, savesRoot, group string
}

func newLayout(t *testing.T) layout {
	t.Helper()
	home := resolve(t.TempDir())
	t.Setenv("HOME", home)
	l := layout{
		home:      home,
		java:      filepath.Join(home, "cache", "java", "21", "bin", "java"),
		lib:       filepath.Join(home, "cache", "libraries", "lwjgl.jar"),
		client:    filepath.Join(home, "cache", "versions", "26.2", "26.2.jar"),
		assets:    filepath.Join(home, "cache", "assets"),
		gameDir:   filepath.Join(home, "instances", "smp"),
		savesRoot: filepath.Join(home, "saves"),
	}
	l.natives = filepath.Join(l.gameDir, ".shulker", "natives")
	l.system = resolve(t.TempDir())
	l.group = filepath.Join(l.savesRoot, "smp")
	for _, dir := range []string{filepath.Dir(l.java), filepath.Dir(l.lib), filepath.Dir(l.client), l.assets, l.gameDir, l.group, filepath.Join(home, ".ssh")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{l.java, l.lib, l.client} {
		if err := os.WriteFile(file, nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return l
}

func (l layout) argv() []string {
	return []string{
		"-Xmx2G", "-Djava.library.path=" + l.natives + string(os.PathListSeparator) + l.system, "-Dorg.lwjgl.system.SharedLibraryExtractPath=" + l.natives,
		"-cp", l.lib + string(os.PathListSeparator) + l.client,
		"net.minecraft.client.main.Main", "--gameDir", l.gameDir, "--assetsDir", l.assets, "--accessToken", "secret",
	}
}

func TestDeriveReadsThePolicyOffTheArgv(t *testing.T) {
	l := newLayout(t)
	if err := os.Symlink(l.group, filepath.Join(l.gameDir, "saves")); err != nil {
		t.Fatal(err)
	}
	instanceFile := filepath.Join(l.gameDir, "instance.json")
	p, err := Derive(l.java, l.argv(), Options{SavesRoot: l.savesRoot, ProtectFiles: []string{instanceFile, filepath.Join(l.home, "elsewhere.json")}})
	if err != nil {
		t.Fatal(err)
	}
	for name, check := range map[string]struct{ got, want []string }{
		// java.library.path is read, never written: it can name a system folder.
		"read":          {p.Read, []string{l.assets, filepath.Join(l.home, "cache", "java", "21"), l.system}},
		"read files":    {p.ReadFiles, []string{l.lib, l.client}},
		"write":         {p.Write, []string{l.gameDir, l.natives, l.group}},
		"protect":       {p.Protect, []string{filepath.Join(l.gameDir, "mods"), filepath.Join(l.gameDir, ".shulker")}},
		"protect files": {p.ProtectFiles, []string{instanceFile}},
		"reopen":        {p.Reopen, []string{filepath.Join(l.gameDir, ".shulker", "logs"), l.natives}},
	} {
		got, want := slices.Sorted(slices.Values(check.got)), slices.Sorted(slices.Values(check.want))
		if !slices.Equal(got, want) {
			t.Errorf("%s:\n got  %v\n want %v", name, got, want)
		}
	}
}

func TestDeriveFollowsASavesLinkOnlyIntoTheSavesRoot(t *testing.T) {
	l := newLayout(t)
	if err := os.Symlink(filepath.Join(l.home, ".ssh"), filepath.Join(l.gameDir, "saves")); err != nil {
		t.Fatal(err)
	}
	p, err := Derive(l.java, l.argv(), Options{SavesRoot: l.savesRoot})
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(p.Write, func(path string) bool { return strings.Contains(path, ".ssh") }) {
		t.Fatalf("a saves link the game repointed must not widen the sandbox: %v", p.Write)
	}
}

func TestDeriveNeverAllowsAFolderHoldingHome(t *testing.T) {
	l := newLayout(t)
	argv := append([]string{"-Djna.tmpdir=" + filepath.Dir(l.home), "-p", l.home}, l.argv()...)
	p, err := Derive(l.java, argv, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range slices.Concat(p.Read, p.Write) {
		if within(l.home, path) {
			t.Errorf("%s holds the home folder", path)
		}
	}
	argv = []string{"-cp", l.lib, "Main", "--gameDir", l.home}
	if _, err := Derive(l.java, argv, Options{}); err == nil {
		t.Fatal("a game directory that is the home folder can't be sandboxed")
	}
	if _, err := Derive(l.java, []string{"-version"}, Options{}); !errors.Is(err, ErrNoGameDir) {
		t.Fatalf("an argv with no game directory: %v", err)
	}
}

func TestProfileDeniesHomeAndCarvesTheInstance(t *testing.T) {
	p := Policy{
		Read:         []string{"/Users/me/cache/java/21"},
		ReadFiles:    []string{`/Users/me/cache/lib "x".jar`},
		Write:        []string{"/Users/me/instances/smp"},
		Protect:      []string{"/Users/me/instances/smp/mods"},
		ProtectFiles: []string{"/Users/me/instances/smp/instance.json"},
		Reopen:       []string{"/Users/me/instances/smp/.shulker/logs"},
	}
	profile := Profile(p)
	order := []string{
		"(allow default)",
		`(deny file-read* file-write*` + "\n" + `  (subpath "/Users")` + "\n" + `  (subpath "/Volumes"))`,
		`(literal "/Users/me/cache/java")`,
		`(allow file-read*` + "\n" + `  (subpath "/Users/me/cache/java/21")` + "\n" + `  (subpath "/Users/me/instances/smp")` + "\n" + `  (literal "/Users/me/cache/lib \"x\".jar"))`,
		`(deny file-write*` + "\n" + `  (subpath "/"))`,
		`(allow file-write*` + "\n" + `  (subpath "/Users/me/instances/smp")`,
		`(deny file-write*` + "\n" + `  (subpath "/Users/me/instances/smp/mods")` + "\n" + `  (literal "/Users/me/instances/smp/instance.json"))`,
		`(allow file-write*` + "\n" + `  (subpath "/Users/me/instances/smp/.shulker/logs"))`,
	}
	at := 0
	for _, part := range order {
		i := strings.Index(profile[at:], part)
		if i < 0 {
			t.Fatalf("profile lacks, or has out of order:\n%s\n\nin:\n%s", part, profile)
		}
		at += i + len(part)
	}
	for _, ancestor := range []string{"/Users", "/Users/me", "/Users/me/cache", "/Users/me/instances"} {
		if !strings.Contains(profile, `(literal "`+ancestor+`")`) {
			t.Errorf("no metadata allow for %s", ancestor)
		}
	}
}
