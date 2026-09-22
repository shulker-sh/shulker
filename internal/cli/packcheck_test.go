package cli

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func lockWarnings(t *testing.T, h *harness, args ...string) []string {
	t.Helper()
	var env out.Envelope
	if err := json.Unmarshal([]byte(h.mustRun(t, append(args, "--json")...)), &env); err != nil {
		t.Fatal(err)
	}
	return env.Warnings
}

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
			"helper: packs/helper has no shaders/ folder at its root, so Iris won't load it",
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.mustRun(t, "init", "--yes", "--loader", "fabric")
			writeFolder(t, filepath.Join(h.dir, "packs", "helper"), tc.files)

			if got := lockWarnings(t, h, tc.kind, "add", "packs/helper"); len(got)+len(tc.want) > 0 && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("add warned %q, want %q", got, tc.want)
			}
			if got := lockWarnings(t, h, "lock"); len(got)+len(tc.want) > 0 && !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("lock warned %q, want %q", got, tc.want)
			}

			l := h.readLock(t)
			locked := l.ResourcePacks["helper"]
			if tc.kind == "shader" {
				locked = l.Shaders["helper"]
			}
			if locked.File != "packs/helper" || locked.Sha512 == "" {
				t.Fatalf("a folder that warns is still locked: %+v", locked)
			}
		})
	}
}
