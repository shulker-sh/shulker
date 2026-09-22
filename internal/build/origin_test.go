package build

import "testing"

func TestMrpackOrigin(t *testing.T) {
	str := func(s string) *string { return &s }
	for _, c := range []struct {
		provider string
		url      *string
		want     string
	}{
		{"modrinth", str("https://cdn.modrinth.com/data/AANobbMI/versions/x/sodium.jar"), "modrinth"},
		{"modrinth", str("https://github.com/owner/repo/releases/download/v1/mod.jar"), "modrinth, github.com"},
		{"curseforge", str("https://edge.forgecdn.net/files/1/2/jei.jar"), "curseforge"},
		{"curseforge", str("https://notforgecdn.net/jei.jar"), "curseforge, notforgecdn.net"},
		{"curseforge", nil, "curseforge, manual download"},
		{"", nil, "local file"},
	} {
		if got := mrpackOrigin(c.provider, c.url); got != c.want {
			t.Errorf("mrpackOrigin(%s, %v) = %q, want %q", c.provider, c.url, got, c.want)
		}
	}
}
