package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	built := filepath.Join(h.dir, "build", "client", "config", "iris.properties")
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "iris.properties"), "enableShaders=true\nshaderPack=pack.zip\n")
	h.mustRun(t, "build")

	writeFile(t, built, "colorSpace=DISPLAY_P3\nenableShaders=false\nshaderPack=pack.zip\n")
	if stdout := h.mustRun(t, "build"); !strings.Contains(stdout, "kept: config/iris.properties enableShaders (edited in place)") {
		t.Fatalf("an in-game edit of a managed key should be kept: %s", stdout)
	}
	if stdout := h.mustRun(t, "pull"); !strings.Contains(stdout, "pulled: config/iris.properties enableShaders → overrides/config/iris.properties") {
		t.Fatalf("pull: %s", stdout)
	}
	if stdout := h.mustRun(t, "pull", "config/iris.properties", "--key", "colorSpace"); !strings.Contains(stdout, "adopted: config/iris.properties colorSpace → overrides/config/iris.properties") {
		t.Fatalf("pull --key: %s", stdout)
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

func TestPropertiesOverrideNothingMergesIntoKeepsItsBytes(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	upstream := "#Iris config\r\nenableShaders: true\r\nshaderPack = pack.zip\r\ncolorSpace=SRGB"
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "iris.properties"), upstream)
	writeFile(t, filepath.Join(h.dir, "overrides", "config", "layered.properties"), "z=1\ny=2\nshaderPack=pack.zip\n")
	writeFile(t, filepath.Join(h.dir, "client-overrides", "config", "layered.properties"), strings.Replace(upstream, "pack.zip", "other.zip", 1))
	layered := "#Iris config\r\nenableShaders=true\nshaderPack=other.zip\ncolorSpace=SRGB\ny=2\nz=1\n"

	h.allowMrpackHost(t)
	h.mustRun(t, "export", "mrpack", "--version", "1.0")
	_, entries := readMrpack(t, filepath.Join(h.dir, "build", "pack-1.0.mrpack"))
	if got := entries["overrides/config/iris.properties"]; got != upstream {
		t.Fatalf("export should carry a file nothing merges into as written: %q (entries: %v)", got, keys(entries))
	}
	if got := entries["overrides/config/layered.properties"]; got != layered {
		t.Fatalf("exported merged file: %q (entries: %v)", got, keys(entries))
	}
}
