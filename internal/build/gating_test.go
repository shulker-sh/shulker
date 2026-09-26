package build

import (
	"slices"
	"testing"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/provider"
)

// gatedProject is a project with sodium locked from Modrinth and fabric-api locked as its
// dependency, with the features fancy, shaders and api declared and off.
func gatedProject(t *testing.T) *testProject {
	t.Helper()
	p := newProject(t)
	p.lockMod("sodium", p.modrinth, p.modrinth.Publish(mod("AANobbMI", "sodium"), provider.Version{ID: "m-sodium-1", Number: "0.9.2", File: provider.File{Filename: "sodium-0.9.2.jar"}}, modJar(t, "sodium", "0.9.2")))
	p.lockDependency("fabric-api", p.modrinth, p.modrinth.Publish(mod("P7dR8mSH", "fabric-api"), provider.Version{ID: "m-api-1", Number: "0.130.0", File: provider.File{Filename: "fabric-api-0.130.0.jar"}}, modJar(t, "fabric-api", "0.130.0")), "sodium")
	p.b.Manifest.Features = map[string]manifest.Feature{"fancy": {}, "shaders": {}, "api": {}}
	return p
}

func (p *testProject) gate(key string, edit func(r *manifest.Require)) {
	r := p.b.Manifest.Requires[key]
	edit(&r)
	p.b.Manifest.Requires[key] = r
}

func (p *testProject) enable(features ...string) {
	for name := range p.b.Manifest.Features {
		p.b.Manifest.Features[name] = manifest.Feature{Default: slices.Contains(features, name)}
	}
}

func TestFeatureConditionsFilterModsAndDependencies(t *testing.T) {
	p := gatedProject(t)
	p.gate("sodium", func(r *manifest.Require) { r.Feature = manifest.StringList{"fancy"} })
	report := p.mustBuild("client", Options{})
	if len(report.Excluded) != 2 || !slices.Contains(report.Excluded, "sodium (needs feature fancy)") || !slices.Contains(report.Excluded, "fabric-api (only required by sodium)") {
		t.Fatalf("gated build: %q", report.Excluded)
	}
	if jars := p.mods(); len(jars) != 0 {
		t.Fatalf("mods dir should be empty, got %v", jars)
	}

	p.enable("fancy")
	if report := p.mustBuild("client", Options{}); len(report.Excluded) != 0 || len(p.mods()) != 2 {
		t.Fatalf("feature on by default: %q %v", report.Excluded, p.mods())
	}

	p.gate("sodium", func(r *manifest.Require) { r.Feature = manifest.StringList{"fancy", "!shaders"} })
	p.enable("fancy", "shaders")
	if report := p.mustBuild("client", Options{}); !slices.Contains(report.Excluded, "sodium (feature shaders is on)") || len(p.mods()) != 0 {
		t.Fatalf("negated feature: %q %v", report.Excluded, p.mods())
	}

	p.gate("sodium", func(r *manifest.Require) { r.Feature = manifest.StringList{"!shaders"} })
	p.enable()
	if report := p.mustBuild("client", Options{}); len(report.Excluded) != 0 || len(p.mods()) != 2 {
		t.Fatalf("negation alone ships by default: %q %v", report.Excluded, p.mods())
	}
}

func TestOSConditionsUseTheBuildMachine(t *testing.T) {
	p := gatedProject(t)
	here := DetectOS()
	other := "windows"
	if here == other {
		other = "linux"
	}

	p.gate("sodium", func(r *manifest.Require) { r.OS = manifest.StringList{here} })
	if report := p.mustBuild("client", Options{}); len(report.Excluded) != 0 {
		t.Fatalf("matching os: %q", report.Excluded)
	}
	p.gate("sodium", func(r *manifest.Require) { r.OS = manifest.StringList{other, "!" + here} })
	if report := p.mustBuild("client", Options{}); !slices.Contains(report.Excluded, "sodium (os is "+here+")") {
		t.Fatalf("negated os: %q", report.Excluded)
	}
	p.gate("sodium", func(r *manifest.Require) { r.OS = manifest.StringList{other} })
	if report := p.mustBuild("client", Options{}); !slices.Contains(report.Excluded, "sodium (needs os "+other+")") {
		t.Fatalf("other os: %q", report.Excluded)
	}
}

func TestGatedDependencyStillShipsWhenRequired(t *testing.T) {
	p := gatedProject(t)
	p.b.Manifest.Requires["fabric-api"] = manifest.Require{Provider: "modrinth", Feature: manifest.StringList{"api"}}
	report := p.mustBuild("client", Options{})
	if len(report.Excluded) != 0 || len(p.mods()) != 2 {
		t.Fatalf("required dependency dropped: %q %v", report.Excluded, p.mods())
	}
	if !slices.Contains(report.Warnings, "fabric-api is gated off (needs feature api) but sodium requires it; included") {
		t.Fatalf("expected a warning, got: %q", report.Warnings)
	}
}
