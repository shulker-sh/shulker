package resolve

import (
	"slices"
	"testing"

	"shulker.sh/shulker/internal/provider"
)

func TestShaderLoaders(t *testing.T) {
	cases := []struct {
		name string
		v    provider.Version
		want []string
	}{
		{name: "modrinth tags", v: provider.Version{Loaders: []string{"oculus", "iris"}}, want: []string{"iris", "oculus"}},
		{name: "vanilla alongside a shader mod", v: provider.Version{Loaders: []string{"vanilla", "canvas"}}, want: []string{"canvas", "vanilla"}},
		{name: "curseforge optifine", v: provider.Version{GameVersions: []string{"26.2", "OptiFine"}}, want: []string{"iris", "oculus"}},
		{name: "curseforge iris and optifine", v: provider.Version{GameVersions: []string{"26.2", "Iris", "OptiFine"}}, want: []string{"iris", "oculus"}},
		{name: "untagged", v: provider.Version{GameVersions: []string{"26.2"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shaderLoaders(&c.v); !slices.Equal(got, c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
		})
	}
}
