package build

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/packarchive"
)

// pullPack adds a directory modpack named name beside the project, with the manifest given, and
// returns it for its files to be written.
func (p *project) pullPack(name string, m *manifest.Manifest) *pack.Loaded {
	p.t.Helper()
	dir := filepath.Join(p.b.Dir, name)
	m.Name, m.Minecraft, m.Loader = name, "~26.2", manifest.Loader{Type: "fabric", Version: "*"}
	if m.Requires == nil {
		m.Requires = map[string]manifest.Require{}
	}
	if m.Client == nil {
		m.Client = &manifest.Client{}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		p.t.Fatal(err)
	}
	if err := m.Save(filepath.Join(dir, manifest.FileName)); err != nil {
		p.t.Fatal(err)
	}
	p.b.Manifest.Requires[name] = manifest.Require{Source: "./" + name}
	l := &pack.Loaded{Name: name, Source: "./" + name, Kind: pack.Local, Dir: dir, Manifest: m}
	p.b.Packs = append(p.b.Packs, l)
	return l
}

// exportMrpack exports the project's client as an mrpack with the version given.
func (p *project) exportMrpack(version string) (*ExportReport, error) {
	p.t.Helper()
	p.save()
	format, _ := packarchive.Lookup("mrpack")
	return p.b.Export(context.Background(), ExportOptions{Format: format, Version: version, Output: filepath.Join(p.b.Dir, "build", "pack-"+version+".mrpack")})
}

func TestPulledPackFeaturesMergeIntoTheProject(t *testing.T) {
	p := newProject(t)
	p.b.Manifest.Features = map[string]manifest.Feature{"shaders": {Default: false, Note: "the project's note"}}
	base := p.pullPack("base", &manifest.Manifest{Features: map[string]manifest.Feature{"shaders": {Default: true, Note: "the pack's note"}, "voice": {}}})
	writeFile(t, filepath.Join(base.Dir, "overrides", "config", "base.txt"), "from the pack\n")
	writeFile(t, filepath.Join(base.Dir, "shaders-overrides", "config", "shade.txt"), "pack\n")
	p.file("shaders-overrides/config/shade.txt", "project\n")

	p.mustBuild("client", Options{})
	if got := p.built("client", "config/base.txt"); got != "from the pack\n" {
		t.Fatalf("the pack's shared overrides: %q", got)
	}
	if p.hasBuilt("client", "config/shade.txt") {
		t.Fatal("the project's default must win the clash, leaving the feature off")
	}

	listed := p.b.Features()
	if len(listed) != 2 || listed[0].Name != "shaders" || listed[0].Default || listed[0].Origin != "" {
		t.Fatalf("one switch for the name both declare, and the project owns it: %+v", listed)
	}
	if listed[1].Name != "voice" || listed[1].Origin != "base" {
		t.Fatalf("a feature only the pack declares keeps its origin: %+v", listed[1])
	}

	p.mustBuild("client", Options{Features: map[string]bool{"shaders": true}})
	if got := p.built("client", "config/shade.txt"); got != "project\n" {
		t.Fatalf("the project's feature folder layers over the pack's: %q", got)
	}
}

func TestBuiltinVariables(t *testing.T) {
	p := newProject(t)
	p.b.Lock.DataVersion = 4903
	p.b.Manifest.Version = "1.2.3"
	p.b.Manifest.Client = &manifest.Client{Name: "Pack Client", Options: map[string]any{"version": "${minecraft.dataVersion}"}}
	p.b.Manifest.Server.Properties = map[string]any{"motd": "${project.name} ${project.version}"}
	base := p.pullPack("base", &manifest.Manifest{})
	writeFile(t, filepath.Join(base.Dir, "overrides", "config", "base.txt.tmpl"), "${project.name} ${project.displayName} ${project.version} ${minecraft.version}\n")
	p.override("config/stamp.txt.tmpl", "${project.name} ${project.displayName} ${project.version} ${minecraft.version} ${minecraft.dataVersion} ${java.major} ${loader.type} ${loader.version}\n")

	_, err := p.build("client", Options{})
	if e := out.AsError(err); e == nil || e.Code != "unset-variable" || e.Message != "base:overrides/config/base.txt.tmpl:1: variable ${project.version} is not set" {
		t.Fatalf("a pulled pack without a version doesn't take the project's: %v", err)
	}

	base.Manifest.Version = "9.0"
	p.mustBuild("client", Options{})
	p.mustBuild("server", Options{NoLauncher: true})
	if got := p.built("client", "config/stamp.txt"); got != "pack Pack Client 1.2.3 26.2 4903 21 fabric 0.17.3\n" {
		t.Errorf("the project's overrides see its own pack and the locked versions: %q", got)
	}
	if got := p.built("client", "config/base.txt"); got != "base base 9.0 26.2\n" {
		t.Errorf("a pulled pack's overrides see that pack's own name and version: %q", got)
	}
	if got := p.built("server", "server.properties"); !strings.Contains(got, "motd=pack 1.2.3\n") {
		t.Errorf("server.properties values see the built-in variables: %q", got)
	}
	if got := p.built("client", "options.txt"); !strings.Contains(got, "version:4903\n") {
		t.Errorf("client.options values see the built-in variables, unquoted: %q", got)
	}

	if _, err := p.exportMrpack("2.0"); err != nil {
		t.Fatal(err)
	}
	archive := unzip(t, []byte(p.project("build/pack-2.0.mrpack")))
	if got := archive["client-overrides/config/stamp.txt"]; got != "pack Pack Client 2.0 26.2 4903 21 fabric 0.17.3\n" {
		t.Errorf("an export's version stands in for the project's: %q", got)
	}
	if got := archive["overrides/config/base.txt"]; got != "base base 9.0 26.2\n" {
		t.Errorf("an export's version leaves a pulled pack's own: %q", got)
	}

	p.b.Manifest.Version = ""
	_, err = p.build("client", Options{})
	if e := out.AsError(err); e == nil || !strings.Contains(e.Message, "${project.version} is not set") || e.Help != "run `shulker set version <version>`" {
		t.Fatalf("a manifest without a version leaves ${project.version} unset: %v", err)
	}

	p.override("config/stamp.txt.tmpl", "${project.nme}\n")
	_, err = p.build("client", Options{})
	if e := out.AsError(err); e == nil || e.Given != "project.nme" || !contains(e.Candidates, "project.name") {
		t.Fatalf("a misspelt variable suggests the closest: %v", err)
	}
}
