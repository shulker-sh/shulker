package build

import "testing"

func TestOriginNamesTheProviderOrHowTheFileCame(t *testing.T) {
	str := func(s string) *string { return &s }
	for _, c := range []struct {
		provider string
		url      *string
		want     string
	}{
		{"modrinth", str("https://cdn.modrinth.test/data/AANobbMI/versions/x/sodium.jar"), "modrinth"},
		{"curseforge", nil, "curseforge, manual download"},
		{"", nil, "local file"},
	} {
		if got := origin(c.provider, c.url); got != c.want {
			t.Errorf("origin(%s, %v) = %q, want %q", c.provider, c.url, got, c.want)
		}
	}
}
