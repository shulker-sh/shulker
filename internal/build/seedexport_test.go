package build

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/packarchive"
)

func TestExportPutsSeededFilesInTheFirstPlacedSeedModsFolder(t *testing.T) {
	cases := []struct {
		name         string
		mods         []string
		integrations map[string][]string
		folder       string
	}{
		{name: "config manager", mods: []string{"config_manager"}, folder: "config/modpack_defaults/"},
		{name: "yosbr only", mods: []string{"yosbr"}, folder: "config/yosbr/"},
		{name: "configured defaults only", mods: []string{"configureddefaults"}, folder: "configureddefaults/"},
		{name: "config manager and yosbr", mods: []string{"yosbr", "config_manager"}, folder: "config/modpack_defaults/"},
		{name: "yosbr and configured defaults", mods: []string{"configureddefaults", "yosbr"}, folder: "config/yosbr/"},
		{name: "none", folder: ""},
		{name: "emptied", mods: []string{"config_manager"}, integrations: map[string][]string{"configmanager": {}}, folder: ""},
	}
	for _, format := range []string{"mrpack", "curseforge"} {
		for _, c := range cases {
			t.Run(format+"/"+c.name, func(t *testing.T) {
				p := newProject(t)
				p.b.Manifest.Server = nil
				p.b.Manifest.SeedFiles = []string{"options.txt", "config/seeded.json"}
				p.b.Manifest.Integrations = c.integrations
				for _, id := range c.mods {
					p.lockLocalMod(id, id+"-1.0.jar", modJar(t, id, "1.0"))
				}
				p.override("options.txt", "lang:en_us\n")
				p.override("config/seeded.json", "{}\n")
				p.override("config/plain.json", "{}\n")
				p.save()
				f, _ := packarchive.Lookup(format)
				report, err := p.b.Export(context.Background(), ExportOptions{Format: f, Version: "1.0", Output: p.archivePath(), Bundle: true})
				if err != nil {
					t.Fatal(err)
				}
				archive := p.archive()
				for _, rel := range []string{"options.txt", "config/seeded.json"} {
					if _, ok := archive["overrides/"+c.folder+rel]; !ok {
						t.Fatalf("%s goes to overrides/%s%s: %v", rel, c.folder, rel, slices.Sorted(maps.Keys(archive)))
					}
					if _, ok := archive["overrides/"+rel]; c.folder != "" && ok {
						t.Fatalf("a relocated %s also ships at its own path", rel)
					}
				}
				if _, ok := archive["overrides/config/plain.json"]; !ok {
					t.Fatal("an unseeded file stays at its own path")
				}
				warned := slices.IndexFunc(report.Warnings, func(w string) bool { return strings.HasPrefix(w, "seeded files ship as plain overrides") })
				if (c.folder == "") != (warned >= 0) {
					t.Fatalf("the warning goes only with no seed mod placed: %v", report.Warnings)
				}
				if warned >= 0 && report.Warnings[warned] != "seeded files ship as plain overrides, which launchers write over the player's copy on each update: config/seeded.json, options.txt; add configmanager, yosbr or configured-defaults to keep them seeded" {
					t.Fatalf("warning: %s", report.Warnings[warned])
				}
			})
		}
	}
}
