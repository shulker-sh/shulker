package build

import (
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/lock"
)

func TestServerJarsFollowTheForgeVersion(t *testing.T) {
	cases := []struct {
		minecraft, version, vanilla string
		launch                      []string
	}{
		{"1.12.2", "14.23.5.2860", "minecraft_server.1.12.2.jar", []string{"-jar", "forge-1.12.2-14.23.5.2860.jar"}},
		{"1.16.5", "36.2.39", "minecraft_server.1.16.5.jar", []string{"-jar", "forge-1.16.5-36.2.39.jar"}},
		{"26.2", "65.1.3", "libraries/net/minecraft/server/26.2/server-26.2-bundled.jar", []string{"@libraries/net/minecraftforge/forge/26.2-65.1.3/"}},
	}
	for _, c := range cases {
		lk := &lock.Lock{Minecraft: c.minecraft, Loader: lock.Loader{Type: "forge", Version: c.version}}
		if got := vanillaServerPath(lk.RunningLoader(), c.minecraft); got != c.vanilla {
			t.Errorf("%s: vanilla jar at %s, want %s", c.minecraft, got, c.vanilla)
		}
		got := LaunchArgs(lk)
		if len(got) != len(c.launch) || !slices.Equal(got[:len(got)-1], c.launch[:len(c.launch)-1]) || !strings.HasPrefix(got[len(got)-1], c.launch[len(c.launch)-1]) {
			t.Errorf("%s: launch args %v, want %v", c.minecraft, got, c.launch)
		}
	}
}
