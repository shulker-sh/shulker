package cli

import (
	"os"
	"path/filepath"
	"slices"
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
	h.mustRun(t, "add", "irisshaders", "--provider", "curseforge", "--as", "fancy-shaders")
	h.editManifest(t, func(m map[string]any) {
		m["requires"].(map[string]any)["fancy-shaders"].(map[string]any)["feature"] = "shaders"
		declarations(m)["shaders"] = map[string]any{}
	})
	h.mustRun(t, "lock")

	h.mustRun(t, "install")
	if readBuilt(t, h, "shaderpacks/complementary-reimagined.zip") == "" {
		t.Fatal("the shader was not placed")
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build", "client", "config", "iris.properties")); !os.IsNotExist(err) {
		t.Fatalf("iris.properties written in a build that placed only canvas: %v", err)
	}

	h.mustRun(t, "feature", "on", "shaders")
	h.mustRun(t, "build")
	if got := readBuilt(t, h, "config/iris.properties"); !strings.Contains(got, "shaderPack=complementary-reimagined.zip") || !strings.Contains(got, "enableShaders=true") {
		t.Fatalf("iris.properties: %q", got)
	}
}

func TestCurseForgeOptiFineShaderLocksIrisAndOculus(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "shader", "add", "complementary-cf", "--provider", "curseforge")
	if got := h.readLock(t).Shaders["complementary-cf"].Loaders; !slices.Equal(got, []string{"iris", "oculus"}) {
		t.Fatalf("loaders: %v", got)
	}
}
