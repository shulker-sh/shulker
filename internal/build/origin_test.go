package build

import (
	"testing"

	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/provider/fake"
)

func TestOriginNamesTheHostOnlyOffTheProvidersOwn(t *testing.T) {
	str := func(s string) *string { return &s }
	ps := provider.Providers{"modrinth": fake.New("modrinth"), "curseforge": fake.New("curseforge")}
	for _, c := range []struct {
		provider string
		url      *string
		want     string
	}{
		{"modrinth", str("https://cdn.modrinth.test/data/AANobbMI/versions/x/sodium.jar"), "modrinth"},
		{"modrinth", str("https://github.com/owner/repo/releases/download/v1/mod.jar"), "modrinth, github.com"},
		{"curseforge", str("https://curseforge.test/files/1/2/jei.jar"), "curseforge"},
		{"curseforge", str("https://notcurseforge.test/jei.jar"), "curseforge, notcurseforge.test"},
		{"curseforge", nil, "curseforge, manual download"},
		{"", nil, "local file"},
	} {
		if got := origin(ps, c.provider, c.url); got != c.want {
			t.Errorf("origin(%s, %v) = %q, want %q", c.provider, c.url, got, c.want)
		}
	}
}
