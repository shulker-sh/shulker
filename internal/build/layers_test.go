package build

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/manifest"
)

func TestOverrideFoldersLayerInOrder(t *testing.T) {
	p := newProject(t)
	p.b.Manifest.Features = map[string]manifest.Feature{"alpha": {Default: true}, "zulu": {Default: true}, "off": {}}
	p.file("overrides/config/layered.txt", "base\n")
	p.file("overrides/config/shared.txt", "shared\n")
	p.file("client-overrides/config/layered.txt", "client\n")
	p.file("server-overrides/config/layered.txt", "server\n")
	p.file("alpha-overrides/config/layered.txt", "alpha\n")
	p.file("zulu-overrides/config/layered.txt", "zulu\n")
	p.file("off-overrides/config/never.txt", "off\n")

	report := p.mustBuild("client", Options{})
	if got := p.built("client", "config/layered.txt"); got != "zulu\n" {
		t.Fatalf("last enabled feature wins: %q", got)
	}
	if got := p.built("client", "config/shared.txt"); got != "shared\n" {
		t.Fatalf("shared override: %q", got)
	}
	if p.hasBuilt("client", "config/never.txt") {
		t.Fatal("a feature that is off must not lay down its folder")
	}
	if !contains(report.Warnings, "alpha and zulu both write config/layered.txt; zulu wins") {
		t.Fatalf("feature conflict warning: %q", report.Warnings)
	}
	if contains(report.Warnings, "client") {
		t.Fatalf("a feature overriding a base layer says nothing: %q", report.Warnings)
	}
}

func TestOverrideFoldersFollowTheSide(t *testing.T) {
	p := newProject(t)
	p.b.Manifest.Variables = manifest.Variables{"motd": "shared"}
	p.b.Manifest.Server.Variables = manifest.Variables{"motd": "the server"}
	p.b.Manifest.Server.Properties = map[string]any{"motd": "${motd}"}
	p.file("overrides/config/both.txt", "both\n")
	p.file("client-overrides/config/client.txt", "client\n")
	p.file("server-overrides/config/server.txt", "server\n")
	p.file("overrides/config/vars.txt.tmpl", "${motd}\n")
	p.mustBuild("client", Options{})
	p.mustBuild("server", Options{NoLauncher: true})

	for _, tc := range []struct {
		side, rel string
		want      bool
	}{
		{"client", "config/both.txt", true},
		{"client", "config/client.txt", true},
		{"client", "config/server.txt", false},
		{"server", "config/both.txt", true},
		{"server", "config/server.txt", true},
		{"server", "config/client.txt", false},
	} {
		if got := p.hasBuilt(tc.side, tc.rel); got != tc.want {
			t.Errorf("%s %s present = %v, want %v", tc.side, tc.rel, got, tc.want)
		}
	}
	if got := p.built("server", "config/vars.txt"); got != "the server\n" {
		t.Errorf("the side's variables layer over the project's: %q", got)
	}
	if got := p.built("client", "config/vars.txt"); got != "shared\n" {
		t.Errorf("a side without its own variable keeps the project's: %q", got)
	}
}

func TestFeatureFolderForms(t *testing.T) {
	p := newProject(t)
	p.b.Manifest.Features = map[string]manifest.Feature{
		"minimap": {Default: true, Overrides: manifest.FeatureOverrides{Both: "extras/minimap"}},
		"voice":   {Default: true, Overrides: manifest.FeatureOverrides{Client: "voice-client", Server: "voice-server"}},
	}
	p.file("extras/minimap/config/minimap.txt", "minimap\n")
	p.file("voice-client/config/voice.txt", "client voice\n")
	p.file("voice-server/config/voice.txt", "server voice\n")
	p.file("minimap-overrides/config/unused.txt", "unused\n")
	p.mustBuild("client", Options{})
	p.mustBuild("server", Options{NoLauncher: true})

	if got := p.built("client", "config/minimap.txt"); got != "minimap\n" {
		t.Errorf("a named folder applies to both sides: %q", got)
	}
	if got := p.built("server", "config/minimap.txt"); got != "minimap\n" {
		t.Errorf("a named folder applies to the server too: %q", got)
	}
	if got := p.built("client", "config/voice.txt"); got != "client voice\n" {
		t.Errorf("the client's own folder: %q", got)
	}
	if got := p.built("server", "config/voice.txt"); got != "server voice\n" {
		t.Errorf("the server's own folder: %q", got)
	}
	if p.hasBuilt("client", "config/unused.txt") {
		t.Error("a declared folder replaces the default one")
	}
}

func TestFeaturesSettingOneKeyDifferentlyWarn(t *testing.T) {
	p := newProject(t)
	p.b.Manifest.Features = map[string]manifest.Feature{"alpha": {Default: true}, "zulu": {Default: true}}
	p.file("overrides/config/mod.properties", "base=1\nscale=1\nquiet=1\n")
	p.file("alpha-overrides/config/mod.properties", "scale=2\nquiet=9\n")
	p.file("zulu-overrides/config/mod.properties", "scale=3\nquiet=9\n")

	report := p.mustBuild("client", Options{})
	if !contains(report.Warnings, "alpha and zulu set scale in config/mod.properties differently; zulu wins") {
		t.Fatalf("differing key warns: %q", report.Warnings)
	}
	if contains(report.Warnings, "quiet") {
		t.Fatalf("the same value in both is no conflict: %q", report.Warnings)
	}
	if got := p.built("client", "config/mod.properties"); !strings.Contains(got, "scale=3") || !strings.Contains(got, "base=1") {
		t.Fatalf("merged properties: %q", got)
	}
}

func TestOverrideFoldersSkipSymlinks(t *testing.T) {
	p := newProject(t)
	secret := filepath.Join(t.TempDir(), "id_ed25519")
	writeFile(t, secret, "private key\n")
	p.file("overrides/config/real.txt", "real\n")
	if err := os.Symlink(secret, filepath.Join(p.b.Dir, "overrides", "config", "key")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(secret), filepath.Join(p.b.Dir, "overrides", "linked")); err != nil {
		t.Fatal(err)
	}

	p.mustBuild("client", Options{})
	if p.hasBuilt("client", "config/key") || p.hasBuilt("client", "linked/id_ed25519") {
		t.Fatal("a symlinked override must not be copied into the build")
	}
	if got := p.built("client", "config/real.txt"); got != "real\n" {
		t.Fatalf("regular override: %q", got)
	}
}
