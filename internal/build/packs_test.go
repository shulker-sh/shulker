package build

import (
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
	}{
		{name: "tagged only for oculus, with only iris placed", shaders: map[string][]string{"bsl": {"oculus"}}, placed: []string{"iris"}},
		{name: "local shader with iris", shaders: map[string][]string{"bsl": nil}, placed: []string{"iris"}, file: "config/iris.properties", pack: "bsl.zip"},
		{name: "local shader with oculus", shaders: map[string][]string{"bsl": nil}, placed: []string{"oculus"}, file: "config/oculus.properties", pack: "bsl.zip"},
		{name: "iris before oculus", shaders: map[string][]string{"bsl": {"iris", "oculus"}}, placed: []string{"oculus", "iris"}, file: "config/iris.properties", pack: "bsl.zip"},
		{name: "oculus for a pack both can load", shaders: map[string][]string{"bsl": {"iris", "oculus"}}, placed: []string{"oculus"}, file: "config/oculus.properties", pack: "bsl.zip"},
		{name: "canvas has no config file", shaders: map[string][]string{"bsl": nil}, placed: []string{"canvas"}},
		{name: "no shader mod placed", shaders: map[string][]string{"bsl": {"iris"}}, placed: []string{"sodium"}},
		{name: "first loadable pack wins", shaders: map[string][]string{"alpha": {"oculus"}, "beta": {"iris"}, "gamma": {"iris"}}, placed: []string{"iris"}, file: "config/iris.properties", pack: "beta.zip"},
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
			b.enableShader(desired, placed)
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
