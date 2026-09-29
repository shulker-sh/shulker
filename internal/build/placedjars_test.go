package build

import (
	"os"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/provider"
)

func TestBuildWarnsAboutAPlacedJarThatChangedAndKeepsIt(t *testing.T) {
	p := newProject(t)
	sodium := p.modrinth.Publish(provider.Project{ID: "AANobbMI", Slug: "sodium", Title: "Sodium"}, provider.Version{ID: "QANobbMI", Number: "0.9.2", File: provider.File{Filename: "sodium-fabric-0.9.2+mc26.2.jar"}}, modJar(t, "sodium", "0.9.2"))
	p.lockMod("sodium", p.modrinth, sodium)
	p.override("config/sodium.json", "{\"fast\": true}\n")
	p.save()
	p.mustBuild("client", Options{})
	jar := "mods/" + sodium.File.Filename
	infected := string(p.cdn.Bytes(sodium)) + "stage3"
	if err := os.WriteFile(p.builtPath("client", jar), []byte(infected), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.builtPath("client", "config/sodium.json"), []byte("{\"fast\": false}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	report := p.mustBuild("client", Options{})

	if got := p.built("client", jar); got != infected {
		t.Fatal("the changed jar stays in place")
	}
	if len(report.ChangedJars) != 1 || report.ChangedJars[0] != (ChangedJar{Path: jar, Key: "sodium"}) {
		t.Fatalf("changed jars: %+v", report.ChangedJars)
	}
	if !slices.Equal(report.Kept, []string{"config/sodium.json"}) {
		t.Fatalf("an edited config is still kept, and the jar isn't: %q", report.Kept)
	}
	warnings := report.SecurityWarnings(WarnContext{Force: "shulker build --force"})
	if len(warnings) != 1 || warnings[0].Protection != "placed-jars" {
		t.Fatalf("security warnings: %+v", warnings)
	}
	w := warnings[0]
	if want := "1 jar no longer matches the copy shulker locked; the build kept it as it is.\n" + jar; w.Message != want {
		t.Fatalf("warning %q, want %q", w.Message, want)
	}
	var commands []string
	for _, n := range w.Nudges {
		commands = append(commands, n.Command)
	}
	if want := []string{"shulker audit sodium", "shulker build --force", "shulker security"}; !slices.Equal(commands, want) {
		t.Fatalf("nudges %q, want %q", commands, want)
	}

	report = p.mustBuild("client", Options{Force: true})
	if got := p.built("client", jar); got != string(p.cdn.Bytes(sodium)) {
		t.Fatal("--force puts the locked bytes back")
	}
	if len(report.ChangedJars) != 0 {
		t.Fatalf("a forced build keeps no changed jar: %+v", report.ChangedJars)
	}
}

func TestIsPlacedJarIsOnlyAJarDirectlyInMods(t *testing.T) {
	for rel, want := range map[string]bool{"mods/a.jar": true, "mods/A.JAR": true, "mods/sub/a.jar": false, "config/a.jar": false, "mods/a.txt": false} {
		if got := isPlacedJar(rel); got != want {
			t.Errorf("isPlacedJar(%q) = %v", rel, got)
		}
	}
}
