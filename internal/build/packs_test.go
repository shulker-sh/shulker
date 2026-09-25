package build

import (
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/provider"
)

func TestEnableShader(t *testing.T) {
	cases := []struct {
		name    string
		shaders map[string][]string
		placed  []string
		file    string
		pack    string
		warns   []string
	}{
		{name: "tagged only for oculus, with only iris placed", shaders: map[string][]string{"bsl": {"oculus"}}, placed: []string{"iris"}, warns: []string{unloadable("bsl")}},
		{name: "local shader with iris", shaders: map[string][]string{"bsl": nil}, placed: []string{"iris"}, file: "config/iris.properties", pack: "bsl.zip"},
		{name: "local shader with oculus", shaders: map[string][]string{"bsl": nil}, placed: []string{"oculus"}, file: "config/oculus.properties", pack: "bsl.zip"},
		{name: "iris before oculus", shaders: map[string][]string{"bsl": {"iris", "oculus"}}, placed: []string{"oculus", "iris"}, file: "config/iris.properties", pack: "bsl.zip"},
		{name: "oculus for a pack both can load", shaders: map[string][]string{"bsl": {"iris", "oculus"}}, placed: []string{"oculus"}, file: "config/oculus.properties", pack: "bsl.zip"},
		{name: "canvas has no config file", shaders: map[string][]string{"bsl": nil}, placed: []string{"canvas"}, warns: []string{inCanvas("bsl")}},
		{name: "canvas pack with only iris placed", shaders: map[string][]string{"bsl": {"canvas"}}, placed: []string{"iris"}, warns: []string{unloadable("bsl")}},
		{name: "no shader mod placed", shaders: map[string][]string{"bsl": {"iris"}}, placed: []string{"sodium"}, warns: []string{unloadable("bsl")}},
		{name: "local shader with no shader mod placed", shaders: map[string][]string{"bsl": nil}, placed: nil, warns: []string{unloadable("bsl")}},
		{name: "first loadable pack wins", shaders: map[string][]string{"alpha": {"oculus"}, "beta": {"iris"}, "gamma": {"iris"}}, placed: []string{"iris"}, file: "config/iris.properties", pack: "beta.zip", warns: []string{unloadable("alpha"), inGame("gamma")}},
		{name: "canvas pack beside an iris pick", shaders: map[string][]string{"alpha": {"iris"}, "beta": {"canvas"}}, placed: []string{"iris", "canvas"}, file: "config/iris.properties", pack: "alpha.zip", warns: []string{inCanvas("beta")}},
		{name: "vanilla shader is a resource pack", shaders: map[string][]string{"bsl": {"vanilla"}}, placed: nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := &Builder{Lock: &lock.Lock{Shaders: map[string]lock.Pack{}}}
			desired := map[string]source{}
			for key, loaders := range c.shaders {
				b.Lock.Shaders[key] = lock.Pack{Filename: key + ".zip", Loaders: loaders}
				desired["shaderpacks/"+key+".zip"] = source{}
			}
			placed := map[string]bool{}
			for _, id := range c.placed {
				placed[id] = true
			}
			report := &Report{}
			b.reportUnenabledShaders(desired, placed, b.enableShader(desired, placed), report)
			if !slices.Equal(report.Warnings, c.warns) {
				t.Fatalf("warnings: %q, want %q", report.Warnings, c.warns)
			}
			for _, file := range []string{"config/iris.properties", "config/oculus.properties"} {
				got, written := desired[file]
				if file != c.file {
					if written {
						t.Fatalf("%s written", file)
					}
					continue
				}
				if !written {
					t.Fatalf("%s not written", file)
				}
				if props := got.owned().(propsFile).props; props["shaderPack"] != c.pack || props["enableShaders"] != "true" {
					t.Fatalf("%s: %v", file, props)
				}
			}
		})
	}
}

func unloadable(key string) string {
	return key + " is placed, but nothing in this build can load it; shulker add iris"
}

func inCanvas(key string) string {
	return key + " is placed but not enabled; turn it on in Canvas's own menu"
}

func inGame(key string) string {
	return key + " is placed but not enabled; turn it on in game under Options, Video Settings, Shader Packs"
}

// lockFresh locks a resource pack from the fake CurseForge under key, placed as filename.
func (p *project) lockFresh(key, filename string) {
	p.t.Helper()
	fresh := provider.Project{ID: "600000", Slug: key, Title: "Fresh Animations", Type: manifest.TypeResourcePack}
	p.lockPack(manifest.TypeResourcePack, key, p.cf, p.cf.publish(fresh, provider.Version{ID: "5300001", Number: "1.9.4", Loaders: []string{}, File: provider.File{Filename: "FreshAnimations_CF_v1.9.4.zip"}}, packZip(p.t, "fresh")))
	entry := p.b.Manifest.Requires[key]
	entry.Filename = filename
	p.b.Manifest.Requires[key] = entry
	locked := p.b.Lock.ResourcePacks[key]
	locked.Filename = filename
	p.b.Lock.ResourcePacks[key] = locked
}

func hasWarning(report *Report, text string) bool {
	return slices.ContainsFunc(report.Warnings, func(w string) bool { return strings.Contains(w, text) })
}

func TestPackListBeforeOneThirteenNamesPacksBare(t *testing.T) {
	p := newProject(t)
	p.b.Manifest.Minecraft, p.b.Lock.Minecraft = "1.12.2", "1.12.2"
	p.lockFresh("fresh-animations", "Fresh.zip")

	report := p.mustBuild("client", Options{})
	if got := p.built("client", "options.txt"); !strings.Contains(got, `resourcePacks:["Fresh.zip"]`) {
		t.Fatalf("a 1.12 seed carries bare names and no vanilla entry: %q", got)
	}
	if hasWarning(report, "placed but not enabled") {
		t.Fatalf("the seeded pack was reported: %v", report.Warnings)
	}

	p.lockFresh("fresh-animations", "Fresher.zip")
	report = p.mustBuild("client", Options{})
	if got := p.built("client", "options.txt"); !strings.Contains(got, `resourcePacks:["Fresher.zip"]`) {
		t.Fatalf("a renamed pack is swapped by its bare name: %q", got)
	}
	if hasWarning(report, "placed but not enabled") {
		t.Fatalf("the renamed pack was reported: %v", report.Warnings)
	}
}

func TestShippedPackListBeforeOneThirteenIsCheckedByBareNames(t *testing.T) {
	p := newProject(t)
	p.b.Manifest.Minecraft, p.b.Lock.Minecraft = "1.12.2", "1.12.2"
	p.lockFresh("fresh-animations", "Fresh.zip")
	p.file("overrides/options.txt", "resourcePacks:[\"Fresh.zip\",\"Missing.zip\"]\n")

	report := p.mustBuild("client", Options{})
	if hasWarning(report, "placed but not enabled") {
		t.Fatalf("a pack the shipped list enables was reported: %v", report.Warnings)
	}
	if !hasWarning(report, "options.txt enables Missing.zip, but no pack is placed under that name") {
		t.Fatalf("no warning for a shipped entry nothing places: %v", report.Warnings)
	}
}

func TestPackListFromOneThirteenKeepsTheFilePrefix(t *testing.T) {
	p := newProject(t)
	p.b.Manifest.Minecraft, p.b.Lock.Minecraft = "1.13-pre1", "1.13-pre1"
	p.lockFresh("fresh-animations", "Fresh.zip")

	report := p.mustBuild("client", Options{})
	if got := p.built("client", "options.txt"); !strings.Contains(got, `resourcePacks:["vanilla","file/Fresh.zip"]`) {
		t.Fatalf("1.13 seeds file/ entries after vanilla: %q", got)
	}
	if hasWarning(report, "placed but not enabled") {
		t.Fatalf("the seeded pack was reported: %v", report.Warnings)
	}
}
