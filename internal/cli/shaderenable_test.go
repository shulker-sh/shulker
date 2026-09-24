package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShaderIsEnabledByTheShaderModTheBuildPlaced(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "shader", "add", "complementary-reimagined")
	canvas := makeJar(t, "canvas", "canvas-fabric-26.2.jar", "client")
	writeProjectFile(t, h, "files/canvas-fabric-26.2.jar", canvas.data)
	h.mustRun(t, "add", "files/canvas-fabric-26.2.jar")
	h.mustRun(t, "add", "irisshaders", "--as", "fancy-shaders")
	h.editManifest(t, func(m map[string]any) {
		m["requires"].(map[string]any)["fancy-shaders"].(map[string]any)["feature"] = "shaders"
		declarations(m)["shaders"] = map[string]any{}
	})
	h.mustRun(t, "lock")

	_, _, stderr := h.run(t, "install")
	if !strings.Contains(stderr, "! complementary-reimagined is placed, but nothing in this build can load it; shulker add iris") {
		t.Fatalf("no unloadable line with iris turned off: %s", stderr)
	}
	if readBuilt(t, h, "shaderpacks/ComplementaryReimagined_r5.5.1.zip") == "" {
		t.Fatal("the shader was not placed")
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build", "client", "config", "iris.properties")); !os.IsNotExist(err) {
		t.Fatalf("iris.properties written in a build that placed only canvas: %v", err)
	}

	h.mustRun(t, "feature", "on", "shaders")
	if _, _, stderr := h.run(t, "build"); strings.Contains(stderr, "complementary-reimagined is placed") {
		t.Fatalf("the enabled shader was reported: %s", stderr)
	}
	if got := readBuilt(t, h, "config/iris.properties"); !strings.Contains(got, "shaderPack=ComplementaryReimagined_r5.5.1.zip") || !strings.Contains(got, "enableShaders=true") {
		t.Fatalf("iris.properties: %q", got)
	}
}

func TestShaderWithNoShaderModIsReported(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "shader", "add", "complementary-reimagined")
	_, _, stderr := h.run(t, "install")
	if !strings.Contains(stderr, "! complementary-reimagined is placed, but nothing in this build can load it; shulker add iris") {
		t.Fatalf("no unloadable line: %s", stderr)
	}
}
