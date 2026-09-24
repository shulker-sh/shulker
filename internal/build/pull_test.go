package build

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

func TestPullToChoosesTheOverrideFolder(t *testing.T) {
	p := newProject(t)
	p.b.Manifest.Features = map[string]manifest.Feature{
		"shaders": {Default: true},
		"voice":   {Default: true, Overrides: manifest.FeatureOverrides{Client: "voice-client"}},
	}
	p.override("config/plain.txt", "a=1\n")
	p.mustBuild("client", Options{})

	p.writeBuilt("client", "config/plain.txt", "a=2\n")
	p.mustPull("client", PullRequest{To: "client"})
	if p.project("client-overrides/config/plain.txt") != "a=2\n" || p.project("overrides/config/plain.txt") != "a=1\n" {
		t.Fatalf("--to client writes the side folder and leaves the shared one: %q %q", p.project("client-overrides/config/plain.txt"), p.project("overrides/config/plain.txt"))
	}

	p.writeBuilt("client", "config/plain.txt", "a=3\n")
	p.mustPull("client", PullRequest{})
	if p.project("client-overrides/config/plain.txt") != "a=3\n" || p.project("overrides/config/plain.txt") != "a=1\n" {
		t.Fatalf("a file is updated in the folder that already holds it: %q %q", p.project("client-overrides/config/plain.txt"), p.project("overrides/config/plain.txt"))
	}

	p.writeBuilt("client", "config/new.txt", "new\n")
	p.mustPull("client", PullRequest{Files: []string{"config/new.txt"}})
	if got := p.project("overrides/config/new.txt"); got != "new\n" {
		t.Fatalf("a new file lands in overrides/: %q", got)
	}

	p.writeBuilt("client", "config/shaders.txt", "s\n")
	p.mustPull("client", PullRequest{Files: []string{"config/shaders.txt"}, To: "shaders"})
	if got := p.project("shaders-overrides/config/shaders.txt"); got != "s\n" {
		t.Fatalf("--to feature writes the feature's default folder: %q", got)
	}

	p.writeBuilt("client", "config/voice.txt", "v\n")
	p.mustPull("client", PullRequest{Files: []string{"config/voice.txt"}, To: "voice"})
	if got := p.project("voice-client/config/voice.txt"); got != "v\n" {
		t.Fatalf("--to feature picks the object form's path for the side pulled from: %q", got)
	}

	p.writeBuilt("client", "config/voice.txt", "v2\n")
	_, err := p.pull("client", PullRequest{To: "nope"})
	var e *out.Error
	if !errors.As(err, &e) || e.Code != "usage" || strings.Join(e.Candidates, ",") != "client,shaders,voice" {
		t.Fatalf("unknown --to: %v", err)
	}
}

func TestDriftedDirPicksTheDirectoryWithEdits(t *testing.T) {
	p := newProject(t)
	p.override("config/plain.txt", "a=1\n")
	own := p.builtPath("client", "")
	other := filepath.Join(t.TempDir(), "game")
	p.mustBuild("client", Options{})
	p.mustBuild("client", Options{Dir: other})

	if dir, _, err := p.b.DriftedDir("client", own, []string{own, other}, nil, nil); err != nil || dir != own {
		t.Fatalf("no edits anywhere takes the first: %q %v", dir, err)
	}
	writeFile(t, filepath.Join(other, "config", "plain.txt"), "a=2\n")
	if dir, _, err := p.b.DriftedDir("client", own, []string{own, other}, nil, nil); err != nil || dir != other {
		t.Fatalf("the directory with edits wins: %q %v", dir, err)
	}
	if dir, _, err := p.b.DriftedDir("client", own, []string{own, other}, []string{"config/other.txt"}, nil); err != nil || dir != own {
		t.Fatalf("edits to a file not named don't count: %q %v", dir, err)
	}
	p.writeBuilt("client", "config/plain.txt", "a=3\n")
	_, _, err := p.b.DriftedDir("client", own, []string{own, other}, nil, nil)
	if out.CodeOf(err) != "ambiguous-into" {
		t.Fatalf("edits in both is ambiguous, got %v", err)
	}
	if dir, _, err := p.b.DriftedDir("client", own, []string{own}, nil, nil); err != nil || dir != own {
		t.Fatalf("one directory is taken as is: %q %v", dir, err)
	}
}
