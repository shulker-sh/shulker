package loader

import (
	"slices"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func TestForLegacyForge(t *testing.T) {
	cases := []struct {
		minecraft      string
		metadata       []string
		marker         string
		rootServerJars bool
	}{
		{"1.12.2", []string{"mcmod.info"}, "", true},
		{"1.7.10", []string{"mcmod.info"}, "", true},
		{"1.16.5", []string{"META-INF/mods.toml"}, "META-INF/mods.toml", true},
		{"1.17.1", []string{"META-INF/mods.toml"}, "META-INF/mods.toml", false},
		{"26.2", []string{"META-INF/mods.toml"}, "META-INF/mods.toml", false},
		{"not-a-version", []string{"META-INF/mods.toml"}, "META-INF/mods.toml", false},
	}
	for _, c := range cases {
		l, ok := For("forge", c.minecraft)
		if !ok {
			t.Fatalf("%s: forge not found", c.minecraft)
		}
		if !slices.Equal(l.MetadataFiles, c.metadata) || l.MarkerFile != c.marker || l.RootServerJars != c.rootServerJars {
			t.Errorf("%s: got metadata %v, marker %q, root server jars %v", c.minecraft, l.MetadataFiles, l.MarkerFile, l.RootServerJars)
		}
		if want := c.metadata[0] == "mcmod.info"; l.ModAnnotations != want {
			t.Errorf("%s: mod annotations %v", c.minecraft, l.ModAnnotations)
		}
	}
}

func TestSupportsModernForgeInstallersOnly(t *testing.T) {
	cases := []struct {
		name, minecraft, version string
		ok                       bool
	}{
		{"forge", "1.12.2", "14.23.5.2851", true},
		{"forge", "1.12.2", "14.23.5.2860", true},
		{"forge", "1.12.2", "14.23.5.2847", false},
		{"forge", "1.12.2", "14.23.4.2705", false},
		{"forge", "1.7.10", "10.13.4.1614", false},
		{"forge", "1.16.5", "36.2.39", true},
		{"neoforge", "1.7.10", "1", true},
	}
	for _, c := range cases {
		err := Supports(c.name, c.minecraft, c.version)
		if ok := err == nil; ok != c.ok {
			t.Errorf("%s %s %s: got %v", c.name, c.minecraft, c.version, err)
		}
		if err != nil && out.CodeOf(err) != "loader-version-unsupported" {
			t.Errorf("%s %s: code %q", c.minecraft, c.version, out.CodeOf(err))
		}
	}
}

func TestForLeavesOtherLoadersAlone(t *testing.T) {
	l, _ := For("neoforge", "1.12.2")
	want, _ := Lookup("neoforge")
	if !slices.Equal(l.MetadataFiles, want.MetadataFiles) || l.MarkerFile != want.MarkerFile || l.RootServerJars {
		t.Errorf("neoforge changed: %+v", l)
	}
	if _, ok := For("nope", "1.12.2"); ok {
		t.Error("unknown loader found")
	}
}

func TestServerJarNames(t *testing.T) {
	l, _ := For("forge", "1.12.2")
	if got := VanillaServerJar("1.12.2"); got != "minecraft_server.1.12.2.jar" {
		t.Errorf("vanilla jar %q", got)
	}
	if got := l.InstalledServerJar("1.12.2", "14.23.5.2860"); got != "forge-1.12.2-14.23.5.2860.jar" {
		t.Errorf("installed jar %q", got)
	}
}
