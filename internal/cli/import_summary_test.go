package cli

import (
	"testing"

	"shulker.sh/shulker/internal/resolve"
)

func TestLockedSummary(t *testing.T) {
	files := func(counts ...any) []resolve.LockedFile {
		var fs []resolve.LockedFile
		for i := 0; i < len(counts); i += 3 {
			for range counts[i+2].(int) {
				fs = append(fs, resolve.LockedFile{Type: counts[i].(string), Provider: counts[i+1].(string)})
			}
		}
		return fs
	}
	for _, tc := range []struct {
		name  string
		files []resolve.LockedFile
		want  string
	}{
		{"none", nil, "0 files locked"},
		{"one provider", files("mod", "modrinth", 3), "3 mods locked from Modrinth"},
		{"one provider, every type", files("mod", "modrinth", 2, "resourcepack", "modrinth", 1, "shader", "modrinth", 2), "2 mods, 1 resource pack, 2 shaders locked from Modrinth"},
		{"packs only", files("shader", "curseforge", 1), "1 shader locked from CurseForge"},
		{
			"mixed providers",
			files("mod", "modrinth", 312, "mod", "curseforge", 20, "resourcepack", "modrinth", 11, "shader", "modrinth", 1),
			"332 mods (312 Modrinth, 20 CurseForge), 11 resource packs (Modrinth), 1 shader (Modrinth) locked",
		},
		{"larger provider first", files("mod", "modrinth", 1, "mod", "curseforge", 2), "3 mods (2 CurseForge, 1 Modrinth) locked"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := lockedSummary(tc.files); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
