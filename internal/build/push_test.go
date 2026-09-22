package build

import (
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

const freshSha1 = "0123456789abcdef0123456789abcdef01234567"

func pushBuilder(entry manifest.Require, pack lock.Pack, props map[string]any) *Builder {
	entry.Type = manifest.TypeResourcePack
	return &Builder{
		Manifest: &manifest.Manifest{
			Requires: map[string]manifest.Require{"fresh": entry},
			Server:   &manifest.Server{ResourcePack: "fresh", Properties: props},
		},
		Lock: &lock.Lock{Minecraft: "26.2", ResourcePacks: map[string]lock.Pack{"fresh": pack}},
	}
}

func distributed() lock.Pack {
	url := "https://cdn.modrinth.com/data/fresh/fresh.zip"
	return lock.Pack{Provider: "modrinth", Filename: "fresh.zip", URL: &url, Sha1: freshSha1, Size: 1024}
}

func collectPush(t *testing.T, b *Builder, cond conditions) (properties, *Report, error) {
	t.Helper()
	desired := map[string]source{}
	report := &Report{}
	if _, err := b.collectServer(desired, nil, cond, true, report); err != nil {
		return nil, report, err
	}
	return desired[PropertiesFile].owned.(propsFile).props, report, nil
}

func TestPushResourcePackWritesBothKeys(t *testing.T) {
	b := pushBuilder(manifest.Require{}, distributed(), map[string]any{"require-resource-pack": true})
	props, report, err := collectPush(t, b, conditions{})
	if err != nil {
		t.Fatal(err)
	}
	if props["resource-pack"] != *distributed().URL || props["resource-pack-sha1"] != freshSha1 || props["require-resource-pack"] != "true" {
		t.Fatalf("props: %v", props)
	}
	if len(report.Warnings) > 0 || len(report.Excluded) > 0 {
		t.Fatalf("report: %+v", report)
	}
}

func TestPushResourcePackFeatureOff(t *testing.T) {
	b := pushBuilder(manifest.Require{Feature: manifest.StringList{"fancy"}}, distributed(), nil)
	props, report, err := collectPush(t, b, conditions{features: map[string]bool{"fancy": false}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := props["resource-pack"]; ok {
		t.Fatalf("pushed a pack its feature turned off: %v", props)
	}
	if !slices.ContainsFunc(report.Excluded, func(s string) bool { return strings.Contains(s, "fresh") && strings.Contains(s, "fancy") }) {
		t.Fatalf("no report line naming the feature: %v", report.Excluded)
	}
}

func TestPushResourcePackIgnoresOS(t *testing.T) {
	b := pushBuilder(manifest.Require{OS: manifest.StringList{"windows"}}, distributed(), nil)
	props, _, err := collectPush(t, b, conditions{os: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	if props["resource-pack"] != *distributed().URL {
		t.Fatalf("os-conditioned pack not pushed: %v", props)
	}
}

func TestPushResourcePackOverSizeWarns(t *testing.T) {
	pack := distributed()
	pack.Size = 251 << 20
	props, report, err := collectPush(t, pushBuilder(manifest.Require{}, pack, nil), conditions{})
	if err != nil {
		t.Fatal(err)
	}
	if props["resource-pack"] == "" || len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0], "250 MiB") {
		t.Fatalf("props %v, warnings %v", props, report.Warnings)
	}
}

func TestPushResourcePackFailures(t *testing.T) {
	local := lock.Pack{File: "packs/fresh.zip", Filename: "fresh.zip", Sha1: freshSha1}
	withheld := lock.Pack{Provider: "curseforge", Filename: "fresh.zip", Sha1: freshSha1}
	cases := []struct {
		name  string
		b     *Builder
		code  string
		check func(*out.Error) bool
	}{
		{name: "not a locked resource pack", b: func() *Builder {
			b := pushBuilder(manifest.Require{}, distributed(), nil)
			b.Manifest.Server.ResourcePack = "stale"
			return b
		}(), code: "resourcepack-not-found", check: func(e *out.Error) bool { return slices.Equal(e.Candidates, []string{"fresh"}) }},
		{name: "not distributed", b: pushBuilder(manifest.Require{}, withheld, nil), code: "resourcepack-not-distributed", check: func(e *out.Error) bool {
			return strings.Contains(e.Message, "fresh") && strings.Contains(e.Message, "CurseForge")
		}},
		{name: "local file", b: pushBuilder(manifest.Require{File: "packs/fresh.zip"}, local, nil), code: "resourcepack-local-file", check: func(e *out.Error) bool {
			return e.Message == "fresh is a local file, so there's no URL for clients to download it from" && e.Help != ""
		}},
		{name: "resource-pack hand-set", b: pushBuilder(manifest.Require{}, distributed(), map[string]any{"resource-pack": "https://example.com/p.zip"}), code: "resourcepack-conflict"},
		{name: "resource-pack-sha1 hand-set", b: pushBuilder(manifest.Require{}, distributed(), map[string]any{"resource-pack-sha1": freshSha1}), code: "resourcepack-conflict"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := collectPush(t, c.b, conditions{})
			e, ok := err.(*out.Error)
			if !ok || e.Code != c.code || (c.check != nil && !c.check(e)) {
				t.Fatalf("got %#v", err)
			}
		})
	}
}
