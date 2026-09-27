package resolve

import (
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/packarchive"
)

func TestSeedModFoldersImportAsSeededFiles(t *testing.T) {
	cases := []struct {
		name      string
		mods      []string
		overrides []string
		want      []string
		seeds     []string
		seeded    map[string]int
		warning   string
	}{
		{
			name:      "config manager",
			mods:      []string{"config_manager"},
			overrides: []string{"overrides/config/modpack_defaults/options.txt", "client-overrides/config/modpack_defaults/config/a.json", "overrides/config/b.json"},
			want:      []string{"client-overrides/config/a.json", "overrides/config/b.json", "overrides/options.txt"},
			seeds:     []string{"options.txt", "config/a.json"},
			seeded:    map[string]int{"config/modpack_defaults": 2},
		},
		{
			name:      "yosbr",
			mods:      []string{"yosbr"},
			overrides: []string{"overrides/config/yosbr/options.txt", "overrides/config/modpack_defaults/x.txt"},
			want:      []string{"overrides/config/modpack_defaults/x.txt", "overrides/options.txt"},
			seeds:     []string{"options.txt"},
			seeded:    map[string]int{"config/yosbr": 1},
		},
		{
			name:      "configured defaults",
			mods:      []string{"configureddefaults"},
			overrides: []string{"overrides/configureddefaults/options.txt"},
			want:      []string{"overrides/options.txt"},
			seeds:     []string{"options.txt"},
			seeded:    map[string]int{"configureddefaults": 1},
		},
		{
			name:      "no seed mod",
			overrides: []string{"overrides/config/modpack_defaults/options.txt"},
			want:      []string{"overrides/config/modpack_defaults/options.txt"},
		},
		{
			name:      "plain and default at one path",
			mods:      []string{"config_manager"},
			overrides: []string{"overrides/config/modpack_defaults/options.txt", "overrides/options.txt"},
			want:      []string{"overrides/options.txt"},
			warning:   "Dropped 1 default in config/modpack_defaults that the pack also ships\noptions.txt",
		},
		{
			name:      "two seed mods at one path",
			mods:      []string{"config_manager", "yosbr"},
			overrides: []string{"overrides/config/modpack_defaults/options.txt", "overrides/config/yosbr/options.txt"},
			want:      []string{"overrides/options.txt"},
			seeds:     []string{"options.txt"},
			warning:   "overrides/config/yosbr/options.txt: dropped, since config/modpack_defaults/options.txt already seeds options.txt",
		},
		{
			name:      "a plain copy for the other side",
			mods:      []string{"config_manager"},
			overrides: []string{"client-overrides/config/modpack_defaults/options.txt", "server-overrides/options.txt"},
			want:      []string{"client-overrides/options.txt", "server-overrides/options.txt"},
			seeds:     []string{"options.txt"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := &manifest.Manifest{}
			l := lock.New()
			for _, id := range c.mods {
				l.Mods[id] = lock.Mod{}
			}
			var overrides []packarchive.Override
			for _, o := range c.overrides {
				layer, p, _ := strings.Cut(o, "/")
				overrides = append(overrides, packarchive.Override{Layer: layer, Path: p})
			}
			rep := &Imported{}
			var got []string
			for _, o := range seedFromSeedMods(m, l, overrides, rep) {
				got = append(got, o.Layer+"/"+o.Path)
			}
			slices.Sort(got)
			if !slices.Equal(got, c.want) {
				t.Fatalf("overrides = %v, want %v", got, c.want)
			}
			if !slices.Equal(m.SeedFiles, c.seeds) {
				t.Fatalf("seedFiles = %v, want %v", m.SeedFiles, c.seeds)
			}
			for folder, n := range c.seeded {
				if len(rep.Seeded[folder]) != n {
					t.Fatalf("seeded = %v", rep.Seeded)
				}
			}
			if c.warning != "" && !slices.Contains(rep.Warnings, c.warning) {
				t.Fatalf("warnings = %v", rep.Warnings)
			}
		})
	}
}
