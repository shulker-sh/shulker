package server

import (
	"reflect"
	"testing"

	"github.com/andrewmast/shulker/internal/mcver"
)

func TestCheckPropertyKeys(t *testing.T) {
	cases := []struct {
		minecraft string
		keys      []string
		problems  []string
		warnings  []string
	}{
		{"26.2", []string{"motd", "view-distance", "chat-spam-threshold-seconds"}, nil, nil},
		{"26.2", []string{"pvp", "spawn-monsters", "spawn-npcs"},
			[]string{"pvp (removed in 1.21.9; use the pvp game rule)", "spawn-monsters (removed in 1.21.9; use the spawnMonsters game rule)", "spawn-npcs (removed in 1.21.2)"}, nil},
		{"1.21.8", []string{"pvp", "spawn-npcs"}, []string{"spawn-npcs (removed in 1.21.2)"}, nil},
		{"1.21.1", []string{"pvp", "spawn-npcs", "pause-when-empty-seconds"}, nil,
			[]string{`server.properties key "pause-when-empty-seconds" was added in Minecraft 1.21.2 and is ignored by 1.21.1`}},
		{"1.19.2", []string{"previews-chat"}, nil, nil},
		{"1.19.3", []string{"previews-chat"}, []string{"previews-chat (removed in 1.19.3)"}, nil},
		{"26.2", []string{"vew-distance", "xyz"}, nil,
			[]string{`server.properties key "vew-distance" is not a known key; did you mean "view-distance"?`, `server.properties key "xyz" is not a known key`}},
	}
	for _, c := range cases {
		got := CheckPropertyKeys(c.keys, mcver.MustParse(c.minecraft))
		if !reflect.DeepEqual(got.Problems, c.problems) || !reflect.DeepEqual(got.Warnings, c.warnings) {
			t.Errorf("%s %v:\n problems %q\n warnings %q", c.minecraft, c.keys, got.Problems, got.Warnings)
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
		for _, v := range []string{p.Since, p.Until} {
			if v != "" {
				if _, err := mcver.Parse(v); err != nil {
					t.Errorf("%s: %v", p.Key, err)
				}
			}
		}
	}
}
