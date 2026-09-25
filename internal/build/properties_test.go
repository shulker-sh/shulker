package build

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/out"
)

func TestPropertiesOverridesMergePerKey(t *testing.T) {
	p := newProject(t)
	const rel = "config/iris.properties"
	p.override(rel, "# shaders on by default\nenableShaders=true\nshaderPack=pack.zip\n")

	p.mustBuild("client", Options{})
	if got := p.built("client", rel); got != "# shaders on by default\nenableShaders=true\nshaderPack=pack.zip\n" {
		t.Fatalf("fresh build should keep the override's layout: %q", got)
	}

	p.writeBuilt("client", rel, "#Iris config\ncolorSpace=SRGB\nenableShaders=false\nmaxShadowRenderDistance=32\n")
	if err := os.Remove(instance.StatePath(p.builtPath("client", ""))); err != nil {
		t.Fatal(err)
	}
	p.mustBuild("client", Options{})
	if got := p.built("client", rel); got != "#Iris config\ncolorSpace=SRGB\nenableShaders=true\nmaxShadowRenderDistance=32\nshaderPack=pack.zip\n" {
		t.Fatalf("a file shulker never wrote should get the override's keys merged in: %q", got)
	}

	p.writeBuilt("client", rel, "#Iris config\ncolorSpace=DISPLAY_P3\nenableShaders=false\nmaxShadowRenderDistance=32\nshaderPack=pack.zip\n")
	if report := p.mustBuild("client", Options{}); !slices.Contains(report.Kept, rel+" enableShaders (edited in place)") {
		t.Fatalf("an in-game edit of a managed key should be kept: %q", report.Kept)
	}
	if got := p.built("client", rel); !strings.Contains(got, "enableShaders=false") || !strings.Contains(got, "colorSpace=DISPLAY_P3") {
		t.Fatalf("build overwrote in-game edits: %q", got)
	}
	if d := p.diff("client"); len(d.Files) != 1 || !strings.Contains(d.Files[0].Diff, "-enableShaders=true\n") || !strings.Contains(d.Files[0].Diff, "+enableShaders=false\n") || strings.Contains(d.Files[0].Diff, "+colorSpace") || strings.Contains(d.Files[0].Diff, "-colorSpace") {
		t.Fatalf("diff should show only the managed key: %+v", d.Files)
	}

	if report := p.mustPull("client", PullRequest{}); !slices.Contains(report.Pulled, rel+" enableShaders -> overrides/"+rel) {
		t.Fatalf("pull: %+v", report)
	}
	if got := p.project("overrides/" + rel); got != "# shaders on by default\nenableShaders=false\nshaderPack=pack.zip\n" {
		t.Fatalf("pull should write only the managed key, in place: %q", got)
	}
	if d := p.diff("client"); len(d.Files) != 0 {
		t.Fatalf("diff after pull: %+v", d.Files)
	}

	if report := p.mustPull("client", PullRequest{Files: []string{rel}, Keys: []string{"colorSpace"}}); !slices.Contains(report.Adopted, rel+" colorSpace -> overrides/"+rel) {
		t.Fatalf("pull --key: %+v", report)
	}
	if got := p.project("overrides/" + rel); got != "# shaders on by default\nenableShaders=false\nshaderPack=pack.zip\ncolorSpace=DISPLAY_P3\n" {
		t.Fatalf("adopted key: %q", got)
	}
	if report := p.mustBuild("client", Options{}); contains(report.Kept, "edited in place") {
		t.Fatalf("an adopted key should be in step with the build: %q", report.Kept)
	}

	p.writeBuilt("client", "config/other.properties", "a=1\nb=2\n")
	p.mustPull("client", PullRequest{Files: []string{"config/other.properties"}, Keys: []string{"b"}})
	if got := p.project("overrides/config/other.properties"); got != "b=2\n" {
		t.Fatalf("adopting into a new override file: %q", got)
	}

	p.writeBuilt("client", "options.txt", "lang:en_us\n")
	for _, tc := range []struct {
		req  PullRequest
		code string
	}{
		{PullRequest{Files: []string{rel}, Keys: []string{"nope"}}, "key-not-found"},
		{PullRequest{Files: []string{"options.txt"}, Keys: []string{"x"}}, "usage"},
		{PullRequest{Files: []string{"config/a.properties", "config/b.properties"}, Keys: []string{"x"}}, "usage"},
	} {
		if _, err := p.pull("client", tc.req); out.CodeOf(err) != tc.code {
			t.Fatalf("%+v: %v", tc.req, err)
		}
	}
}

func TestPropertiesLayersAndWholeFiles(t *testing.T) {
	p := newProject(t)
	p.override("config/mod.properties", "a=base\nb=base\n")
	p.file("client-overrides/config/mod.properties", "b=extra\nc=extra\n")
	p.override("config/strict.properties", "x=1\n")
	p.writeBuilt("client", "config/mod.properties", "a=game\nz=game\n")
	p.writeBuilt("client", "config/strict.properties", "x=0\ny=0\n")

	p.b.Manifest.WholeFiles = []string{"config/strict.*"}
	_, err := p.build("client", Options{})
	var e *out.Error
	if !errors.As(err, &e) || e.Code != "build-conflict" || strings.Join(e.Items, ",") != "config/strict.properties (not written by shulker)" {
		t.Fatalf("a whole file must still conflict with one shulker never wrote: %v", err)
	}
	if got := p.built("client", "config/mod.properties"); got != "a=game\nz=game\n" {
		t.Fatalf("a failed build must not write: %q", got)
	}

	p.removeBuilt("client", "config/strict.properties")
	p.mustBuild("client", Options{})
	if got := p.built("client", "config/mod.properties"); got != "a=base\nz=game\nb=extra\nc=extra\n" {
		t.Fatalf("layers should union with the later one winning: %q", got)
	}
	if got := p.built("client", "config/strict.properties"); got != "x=1\n" {
		t.Fatalf("whole file: %q", got)
	}
}

func TestPropertiesMovingIntoWholeFiles(t *testing.T) {
	p := newProject(t)
	p.override("config/clean.properties", "# clean\r\nb=2\r\na=1\r\n")
	p.override("config/edited.properties", "# edited\nx=1\n")
	p.override("config/gone.properties", "# gone\ng=1\n")
	p.mustBuild("client", Options{})

	p.writeBuilt("client", "config/edited.properties", "# edited\nx=1\ny=player\n")
	if err := os.Remove(filepath.Join(p.b.Dir, "overrides", "config", "gone.properties")); err != nil {
		t.Fatal(err)
	}
	p.b.Manifest.WholeFiles = []string{"config/*.properties"}
	_, err := p.build("client", Options{})
	var e *out.Error
	if !errors.As(err, &e) || e.Code != "build-conflict" || strings.Join(e.Items, ",") != "config/edited.properties (changed in place and in the source)" {
		t.Fatalf("only the file with a key the player added should conflict: %v", err)
	}

	p.writeBuilt("client", "config/edited.properties", "# edited\nx=1\n")
	report := p.mustBuild("client", Options{})
	if got := p.built("client", "config/clean.properties"); got != "# clean\r\nb=2\r\na=1\r\n" {
		t.Fatalf("an untouched merged file should be copied whole: %q", got)
	}
	if p.hasBuilt("client", "config/gone.properties") || contains(report.Kept, "gone.properties (edited") {
		t.Fatalf("an untouched merged file no longer in the source should be removed: %q", report.Kept)
	}

	p.b.Manifest.WholeFiles = nil
	p.mustBuild("client", Options{})
	if d := p.diff("client"); len(d.Files) != 0 {
		t.Fatalf("moving back out of wholeFiles: %+v", d.Files)
	}
}

func TestPropertiesOverrideNothingMergesIntoKeepsItsBytes(t *testing.T) {
	p := newProject(t)
	upstream := "#Iris config\r\nenableShaders: true\r\nshaderPack = pack.zip\r\ncolorSpace=SRGB"
	p.override("config/iris.properties", upstream)
	p.override("config/repeated.properties", "shaderPack=pack.zip\n")
	p.file("client-overrides/config/repeated.properties", upstream)
	p.override("config/layered.properties", "z=1\ny=2\nshaderPack=pack.zip\n")
	p.file("client-overrides/config/layered.properties", strings.Replace(upstream, "pack.zip", "other.zip", 1))
	layered := "#Iris config\r\nenableShaders=true\nshaderPack=other.zip\ncolorSpace=SRGB\ny=2\nz=1\n"

	p.mustBuild("client", Options{})
	if got := p.built("client", "config/iris.properties"); got != upstream {
		t.Fatalf("a fresh build should place a file nothing merges into as written: %q", got)
	}
	if got := p.built("client", "config/repeated.properties"); got != upstream {
		t.Fatalf("a lower layer repeating the top file's value merges nothing into it: %q", got)
	}
	if got := p.built("client", "config/layered.properties"); got != layered {
		t.Fatalf("a merged file: %q", got)
	}
	report := p.mustBuild("client", Options{})
	if contains(report.Written, "iris.properties") || contains(report.Kept, "iris.properties") || p.built("client", "config/iris.properties") != upstream {
		t.Fatalf("a rebuild should leave the kept bytes alone: %+v", report)
	}
}
