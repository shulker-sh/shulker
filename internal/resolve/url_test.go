package resolve

import (
	"errors"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func TestParseURL(t *testing.T) {
	cases := []struct {
		arg  string
		want ProviderURL
	}{
		{"https://modrinth.com/mod/sodium", ProviderURL{Provider: "modrinth", Project: "sodium"}},
		{"https://www.modrinth.com/shader/complementary-reimagined/", ProviderURL{Provider: "modrinth", Project: "complementary-reimagined"}},
		{"https://modrinth.com/project/AANobbMI?tab=versions#top", ProviderURL{Provider: "modrinth", Project: "AANobbMI"}},
		{"https://modrinth.com/mod/sodium/version/mc1.21.1-0.6.0-fabric", ProviderURL{Provider: "modrinth", Project: "sodium", Version: "mc1.21.1-0.6.0-fabric"}},
		{"https://cdn.modrinth.com/data/AANobbMI/versions/b70slbHV/sodium-fabric.jar", ProviderURL{Provider: "modrinth", Project: "AANobbMI", Version: "b70slbHV"}},
		{"https://www.curseforge.com/minecraft/mc-mods/jei", ProviderURL{Provider: "curseforge", Project: "jei"}},
		{"https://curseforge.com/minecraft/texture-packs/fresh-animations/files/5000001", ProviderURL{Provider: "curseforge", Project: "fresh-animations", Version: "5000001"}},
		{"https://legacy.curseforge.com/minecraft/mc-mods/balm-fabric/download/5700001", ProviderURL{Provider: "curseforge", Project: "balm-fabric", Version: "5700001"}},
		{"https://www.curseforge.com/projects/500525", ProviderURL{Provider: "curseforge", Project: "500525"}},
	}
	for _, c := range cases {
		t.Run(c.arg, func(t *testing.T) {
			got, ok, err := ParseURL(c.arg)
			if err != nil || !ok || got != c.want {
				t.Fatalf("got %+v ok=%v err=%v, want %+v", got, ok, err, c.want)
			}
		})
	}
}

func TestParseURLLeavesOtherArgumentsAlone(t *testing.T) {
	for _, arg := range []string{"sodium", "238222", "./mods/sodium.jar", "https://github.com/owner/pack.git", "https://example.com/modrinth.com/mod/sodium"} {
		if _, ok, err := ParseURL(arg); ok || err != nil {
			t.Fatalf("%s: ok=%v err=%v, want it left alone", arg, ok, err)
		}
	}
}

func TestParseURLRefusesUnknownShapesOnProviderHosts(t *testing.T) {
	for _, arg := range []string{
		"https://modrinth.com/mods",
		"https://modrinth.com/fr/mod/sodium",
		"https://modrinth.com/mod/sodium/versions",
		"https://modrinth.com/user/jellysquid",
		"https://cdn.modrinth.com/data/AANobbMI/icon.png",
		"https://www.curseforge.com/minecraft/worlds/skyblock",
		"https://www.curseforge.com/minecraft/mc-mods/jei/files",
		"https://www.curseforge.com/minecraft/mc-mods/jei/files/latest",
		"https://www.curseforge.com/projects/jei",
	} {
		_, _, err := ParseURL(arg)
		if out.CodeOf(err) != "usage" {
			t.Fatalf("%s: err=%v, want a usage error", arg, err)
		}
	}
}

func TestParseURLRefusalListsEveryShapeItReads(t *testing.T) {
	_, _, err := ParseURL("https://modrinth.com/mods")
	var e *out.Error
	if !errors.As(err, &e) {
		t.Fatalf("err=%v, want an *out.Error", err)
	}
	listed := strings.Join(e.Items, "\n")
	for _, want := range []string{"project|plugin", "/download/<file id>", "legacy.", "data-packs"} {
		if !strings.Contains(listed, want) {
			t.Errorf("shapes don't mention %q:\n%s", want, listed)
		}
	}
}
