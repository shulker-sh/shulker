package resolve

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

var (
	helperFiles = map[string]string{
		"pack.mcmeta":                    `{"pack":{"pack_format":64,"description":"helper"}}`,
		"assets/modmenu/lang/en_us.json": `{"modmenu.title":"Mods"}`,
	}
	shaderFiles = map[string]string{"shaders/gbuffers_basic.vsh": "// bsl"}
)

func TestPackFolderThatWontLoadWarns(t *testing.T) {
	for _, tc := range []struct {
		name  string
		kind  string
		files map[string]string
		want  []string
	}{
		{"valid pack", "resourcepack", helperFiles, nil},
		{"valid pack on min and max format", "resourcepack", map[string]string{"pack.mcmeta": `{"pack":{"min_format":[88,0],"max_format":90,"description":{"text":"helper"}}}`}, nil},
		{"valid shader", "shader", shaderFiles, nil},
		{"no pack.mcmeta", "resourcepack", map[string]string{"assets/x/lang/en_us.json": "{}"}, []string{
			"helper: packs/helper has no pack.mcmeta at its root, so the game won't load it",
		}},
		{"pack.mcmeta in a subfolder", "resourcepack", map[string]string{"helper/pack.mcmeta": `{"pack":{"pack_format":64,"description":"helper"}}`}, []string{
			"helper: packs/helper has no pack.mcmeta at its root, so the game won't load it",
		}},
		{"not JSON", "resourcepack", map[string]string{"pack.mcmeta": `{"pack":`}, []string{
			"helper: packs/helper/pack.mcmeta is not valid JSON, so the game won't load it",
		}},
		{"no description", "resourcepack", map[string]string{"pack.mcmeta": `{"pack":{"pack_format":64}}`}, []string{
			"helper: packs/helper/pack.mcmeta has no pack.description, so the game won't load it",
		}},
		{"only min_format", "resourcepack", map[string]string{"pack.mcmeta": `{"pack":{"min_format":88,"description":"helper"}}`}, []string{
			"helper: packs/helper/pack.mcmeta has no pack format (min_format and max_format, or pack_format), so the game won't load it",
		}},
		{"no pack object", "resourcepack", map[string]string{"pack.mcmeta": `{}`}, []string{
			"helper: packs/helper/pack.mcmeta has no pack.description, so the game won't load it",
			"helper: packs/helper/pack.mcmeta has no pack format (min_format and max_format, or pack_format), so the game won't load it",
		}},
		{"no shaders folder", "shader", map[string]string{"gbuffers_basic.vsh": "// bsl"}, []string{
			"helper: packs/helper has no shaders/ folder at its root, so no shader mod will load it",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			for rel, body := range tc.files {
				h.writeProjectFile("packs/helper/"+rel, []byte(body))
			}
			if err := os.MkdirAll(filepath.Join(h.r.Dir, "packs", "helper"), 0o755); err != nil {
				t.Fatal(err)
			}

			if err := h.r.Add(context.Background(), filepath.Join(h.r.Dir, "packs", "helper"), AddOptions{Type: tc.kind}); err != nil {
				t.Fatal(err)
			}
			if len(h.r.Warnings)+len(tc.want) > 0 && !reflect.DeepEqual(h.r.Warnings, tc.want) {
				t.Fatalf("add warned %q, want %q", h.r.Warnings, tc.want)
			}
			h.r.Warnings = nil
			h.mustReconcile()
			if len(h.r.Warnings)+len(tc.want) > 0 && !reflect.DeepEqual(h.r.Warnings, tc.want) {
				t.Fatalf("reconcile warned %q, want %q", h.r.Warnings, tc.want)
			}

			locked := h.r.Lock.ResourcePacks["helper"]
			if tc.kind == "shader" {
				locked = h.r.Lock.Shaders["helper"]
			}
			if locked.File != "packs/helper" || locked.Sha512 == "" {
				t.Fatalf("a folder that warns is still locked: %+v", locked)
			}
		})
	}
}
