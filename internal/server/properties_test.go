package server

import (
	"reflect"
	"testing"

	"shulker.sh/shulker/internal/mcver"
)

func TestCheckProperties(t *testing.T) {
	cases := []struct {
		minecraft string
		values    map[string]string
		problems  []string
		warnings  []string
	}{
		{"26.2", map[string]string{"motd": "hi", "view-distance": "12", "chat-spam-threshold-seconds": "10"}, nil, nil},
		{"26.2", map[string]string{"pvp": "true", "spawn-monsters": "true", "spawn-npcs": "true"},
			[]string{"pvp (removed in 1.21.9; use the pvp game rule)", "spawn-monsters (removed in 1.21.9; use the spawnMonsters game rule)", "spawn-npcs (removed in 1.21.2)"}, nil},
		{"1.21.8", map[string]string{"pvp": "true", "spawn-npcs": "true"}, []string{"spawn-npcs (removed in 1.21.2)"}, nil},
		{"1.21.1", map[string]string{"pvp": "true", "spawn-npcs": "true", "pause-when-empty-seconds": "60"}, nil,
			[]string{`server.properties key "pause-when-empty-seconds" was added in Minecraft 1.21.2 and is ignored by 1.21.1`}},
		{"1.19.2", map[string]string{"previews-chat": "false"}, nil, nil},
		{"1.19.3", map[string]string{"previews-chat": "false"}, []string{"previews-chat (removed in 1.19.3)"}, nil},
		{"26.2", map[string]string{"vew-distance": "1", "xyz": "2"}, nil,
			[]string{`server.properties key "vew-distance" is not a known key; did you mean "view-distance"?`, `server.properties key "xyz" is not a known key`}},
		{"26.2", map[string]string{"online-mode": "1", "max-players": "lots", "difficulty": "brutal", "resource-pack-id": "nope", "region-file-compression": "zstd"},
			[]string{`difficulty ("brutal" is not one of peaceful, easy, normal, hard, 0, 1, 2, 3)`, `max-players ("lots" is not an integer)`, `online-mode ("1" is not true or false)`, `region-file-compression ("zstd" is not one of deflate, lz4, none)`, `resource-pack-id ("nope" is not a uuid)`}, nil},
		{"26.2", map[string]string{"view-distance": "64", "op-permission-level": "4", "difficulty": "2", "resource-pack-id": "7f1c2a3b-4d5e-4f60-8a9b-0c1d2e3f4a5b", "max-players": ""}, nil,
			[]string{`server.properties key "view-distance" is 64, outside 3-32; the game clamps it`}},
		{"1.21.1", map[string]string{"pause-when-empty-seconds": "soon"}, []string{`pause-when-empty-seconds ("soon" is not an integer)`},
			[]string{`server.properties key "pause-when-empty-seconds" was added in Minecraft 1.21.2 and is ignored by 1.21.1`}},
	}
	for _, c := range cases {
		got := CheckProperties(c.values, mcver.MustParse(c.minecraft))
		if !reflect.DeepEqual(got.Problems, c.problems) || !reflect.DeepEqual(got.Warnings, c.warnings) {
			t.Errorf("%s %v:\n problems %q\n warnings %q", c.minecraft, c.values, got.Problems, got.Warnings)
		}
	}
}

func TestPropertyTableIsSortedAndUnique(t *testing.T) {
	for i := 1; i < len(Properties); i++ {
		if Properties[i-1].Key >= Properties[i].Key {
			t.Errorf("%q must come after %q", Properties[i].Key, Properties[i-1].Key)
		}
	}
	for _, p := range Properties {
		if p.Type == Enum && len(p.Values) == 0 || p.Type != Enum && len(p.Values) > 0 || p.Bounds != nil && p.Type != Int {
			t.Errorf("%s: inconsistent type data", p.Key)
		}
		for _, v := range []string{p.Since, p.Until} {
			if v != "" {
				if _, err := mcver.Parse(v); err != nil {
					t.Errorf("%s: %v", p.Key, err)
				}
			}
		}
	}
}
