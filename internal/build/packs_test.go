package build

import (
	"slices"
	"testing"

	"shulker.sh/shulker/internal/lock"
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
			b.enableShader(desired, placed, report)
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
				if props := got.owned.(propsFile).props; props["shaderPack"] != c.pack || props["enableShaders"] != "true" {
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
