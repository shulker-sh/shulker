package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/build"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPropertiesOverridesMergePerKey(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	override := filepath.Join(h.dir, "overrides", "config", "iris.properties")
	built := filepath.Join(h.dir, "build", "client", "config", "iris.properties")
	writeFile(t, override, "# shaders on by default\nenableShaders=true\nshaderPack=pack.zip\n")

	h.mustRun(t, "build")
	if got := readFile(t, built); got != "# shaders on by default\nenableShaders=true\nshaderPack=pack.zip\n" {
		t.Fatalf("fresh build should keep the override's layout: %q", got)
	}

	writeFile(t, built, "#Iris config\ncolorSpace=SRGB\nenableShaders=false\nmaxShadowRenderDistance=32\n")
	if err := os.Remove(build.StatePath(filepath.Join(h.dir, "build", "client"))); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "build")
	if got := readFile(t, built); got != "#Iris config\ncolorSpace=SRGB\nenableShaders=true\nmaxShadowRenderDistance=32\nshaderPack=pack.zip\n" {
		t.Fatalf("a file shulker never wrote should get the override's keys merged in: %q", got)
	}

	writeFile(t, built, "#Iris config\ncolorSpace=DISPLAY_P3\nenableShaders=false\nmaxShadowRenderDistance=32\nshaderPack=pack.zip\n")
	if stdout := h.mustRun(t, "build"); !strings.Contains(stdout, "kept: config/iris.properties enableShaders (edited in place)") {
		t.Fatalf("an in-game edit of a managed key should be kept: %s", stdout)
	}
	if got := readFile(t, built); !strings.Contains(got, "enableShaders=false") || !strings.Contains(got, "colorSpace=DISPLAY_P3") {
		t.Fatalf("build overwrote in-game edits: %q", got)
	}
	if stdout := h.mustRun(t, "diff"); !strings.Contains(stdout, "-enableShaders=true\n") || !strings.Contains(stdout, "+enableShaders=false\n") || strings.Contains(stdout, "+colorSpace") || strings.Contains(stdout, "-colorSpace") {
		t.Fatalf("diff should show only the managed key: %s", stdout)
	}

	if stdout := h.mustRun(t, "pull"); !strings.Contains(stdout, "pulled: config/iris.properties enableShaders ⟶ overrides/config/iris.properties") {
		t.Fatalf("pull: %s", stdout)
	}
	if got := readFile(t, override); got != "# shaders on by default\nenableShaders=false\nshaderPack=pack.zip\n" {
		t.Fatalf("pull should write only the managed key, in place: %q", got)
	}
	if stdout := h.mustRun(t, "diff"); !strings.Contains(stdout, "no changes") {
		t.Fatalf("diff after pull: %s", stdout)
	}

	if stdout := h.mustRun(t, "pull", "config/iris.properties", "--key", "colorSpace"); !strings.Contains(stdout, "adopted: config/iris.properties colorSpace ⟶ overrides/config/iris.properties") {
		t.Fatalf("pull --key: %s", stdout)
	}
	if got := readFile(t, override); got != "# shaders on by default\nenableShaders=false\nshaderPack=pack.zip\ncolorSpace=DISPLAY_P3\n" {
		t.Fatalf("adopted key: %q", got)
	}
	if stdout := h.mustRun(t, "build"); strings.Contains(stdout, "edited in place") {
		t.Fatalf("an adopted key should be in step with the build: %s", stdout)
	}

	writeFile(t, filepath.Join(h.dir, "build", "client", "config", "other.properties"), "a=1\nb=2\n")
	h.mustRun(t, "pull", "config/other.properties", "--key", "b")
	if got := readFile(t, filepath.Join(h.dir, "overrides", "config", "other.properties")); got != "b=2\n" {
		t.Fatalf("adopting into a new override file: %q", got)
	}

	for _, tc := range []struct {
		args []string
		code string
	}{
		{[]string{"pull", "config/iris.properties", "--key", "nope"}, "key-not-found"},
		{[]string{"pull", "options.txt", "--key", "x"}, "usage"},
		{[]string{"pull", "config/a.properties", "config/b.properties", "--key", "x"}, "usage"},
	} {
		code, stdout, _ := h.run(t, append(tc.args, "--json")...)
		if e := failureCode(t, stdout); code == 0 || e.Code != tc.code {
			t.Fatalf("%v: exit %d %s", tc.args, code, stdout)
		}
	}
}

func TestPropertiesLayersAndWholeFiles(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "mod.properties"), "a=base\nb=base\n")
	writeFile(t, filepath.Join(h.dir, "client-overrides", "config", "mod.properties"), "b=extra\nc=extra\n")
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "strict.properties"), "x=1\n")
	built := filepath.Join(h.dir, "build", "client", "config")
	writeFile(t, filepath.Join(built, "mod.properties"), "a=game\nz=game\n")
	writeFile(t, filepath.Join(built, "strict.properties"), "x=0\ny=0\n")

	h.editManifest(t, func(m map[string]any) {
		m["wholeFiles"] = []string{"config/strict.*"}
	})
	code, stdout, _ := h.run(t, "build", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "build-conflict" || strings.Join(e.Items, ",") != "config/strict.properties (not written by shulker)" {
		t.Fatalf("a whole file must still conflict with one shulker never wrote: exit %d %s", code, stdout)
	}
	if got := readFile(t, filepath.Join(built, "mod.properties")); got != "a=game\nz=game\n" {
		t.Fatalf("a failed build must not write: %q", got)
	}

	if err := os.Remove(filepath.Join(built, "strict.properties")); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "build")
	if got := readFile(t, filepath.Join(built, "mod.properties")); got != "a=base\nz=game\nb=extra\nc=extra\n" {
		t.Fatalf("layers should union with the later one winning: %q", got)
	}
	if got := readFile(t, filepath.Join(built, "strict.properties")); got != "x=1\n" {
		t.Fatalf("whole file: %q", got)
	}
}

func TestPropertiesMovingIntoWholeFiles(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "clean.properties"), "# clean\r\nb=2\r\na=1\r\n")
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "edited.properties"), "# edited\nx=1\n")
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "gone.properties"), "# gone\ng=1\n")
	built := filepath.Join(h.dir, "build", "client", "config")
	h.mustRun(t, "build")

	writeFile(t, filepath.Join(built, "edited.properties"), "# edited\nx=1\ny=player\n")
	if err := os.Remove(filepath.Join(h.dir, "overrides", "config", "gone.properties")); err != nil {
		t.Fatal(err)
	}
	h.editManifest(t, func(m map[string]any) {
		m["wholeFiles"] = []string{"config/*.properties"}
	})
	code, stdout, _ := h.run(t, "build", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "build-conflict" || strings.Join(e.Items, ",") != "config/edited.properties (changed in place and in the source)" {
		t.Fatalf("only the file with a key the player added should conflict: exit %d %s", code, stdout)
	}

	writeFile(t, filepath.Join(built, "edited.properties"), "# edited\nx=1\n")
	stdout = h.mustRun(t, "build")
	if got := readFile(t, filepath.Join(built, "clean.properties")); got != "# clean\r\nb=2\r\na=1\r\n" {
		t.Fatalf("an untouched merged file should be copied whole: %q", got)
	}
	if _, err := os.Stat(filepath.Join(built, "gone.properties")); !os.IsNotExist(err) || strings.Contains(stdout, "gone.properties (edited") {
		t.Fatalf("an untouched merged file no longer in the source should be removed: %v %s", err, stdout)
	}

	h.editManifest(t, func(m map[string]any) {
		delete(m, "wholeFiles")
	})
	h.mustRun(t, "build")
	if stdout := h.mustRun(t, "diff"); !strings.Contains(stdout, "no changes") {
		t.Fatalf("moving back out of wholeFiles: %s", stdout)
	}
}
