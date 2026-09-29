package cmdlog

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

const fixtureKey = "$2a$10$bL4bIL5pUWqfcO7KQtnMReakwtfHbNKh6v1uTpKlzhwoueEJQnPnm"

var fixtureHome = filepath.FromSlash("/home/steve")

func fixtureRedaction() Redaction {
	return Redaction{Home: fixtureHome, Keys: []string{fixtureKey}}
}

func TestRedactDropsCredentialsFromAGitURL(t *testing.T) {
	e := fixtureRedaction().Entry(Entry{Level: LevelError, Code: "source-fetch-failed", Msg: "can't clone https://ghp_s3cr3t@github.com/org/pack.git: exit status 128"})
	if want := "can't clone https://github.com/org/pack.git: exit status 128"; e.Msg != want {
		t.Fatalf("msg = %q, want %q", e.Msg, want)
	}
}

func TestRedactDropsAUserAndPasswordFromAManifestURL(t *testing.T) {
	e := fixtureRedaction().Entry(Entry{Level: LevelInfo, Msg: "start", Flags: map[string]string{"source": "https://steve:hunter2@packs.example.com/friends/shulker.json?ref=main"}})
	if want := "https://packs.example.com/friends/shulker.json?ref=main"; e.Flags["source"] != want {
		t.Fatalf("source = %q, want %q", e.Flags["source"], want)
	}
}

func TestRedactLeavesAURLWithoutCredentials(t *testing.T) {
	msg := "fetched https://github.com/org/pack.git and https://example.com/a@b/c and git@github.com:org/pack.git"
	if e := fixtureRedaction().Entry(Entry{Msg: msg}); e.Msg != msg {
		t.Fatalf("msg = %q", e.Msg)
	}
}

func TestRedactHidesTheCurseForgeKeyInAURL(t *testing.T) {
	e := fixtureRedaction().Entry(Entry{Level: LevelWarn, Msg: "GET https://api.curseforge.com/v1/mods?key=" + fixtureKey + " failed"})
	if want := "GET https://api.curseforge.com/v1/mods?key=[key] failed"; e.Msg != want {
		t.Fatalf("msg = %q, want %q", e.Msg, want)
	}
}

func TestRedactHidesTheCurseForgeKeyInAMessage(t *testing.T) {
	e := fixtureRedaction().Entry(Entry{Level: LevelInfo, Msg: "start", Cmd: "config set", Flags: map[string]string{"value": fixtureKey}})
	if e.Flags["value"] != "[key]" {
		t.Fatalf("flags = %v", e.Flags)
	}
	e = fixtureRedaction().Entry(Entry{Level: LevelError, Code: "curseforge-key-rejected", Msg: "the API key " + fixtureKey + " was rejected"})
	if want := "the API key [key] was rejected"; e.Msg != want {
		t.Fatalf("msg = %q, want %q", e.Msg, want)
	}
}

func TestRedactShortensTheHomeDirectory(t *testing.T) {
	game := filepath.Join(fixtureHome, "Games", "friends")
	e := fixtureRedaction().Entry(Entry{Instance: game, Msg: "can't write " + filepath.Join(game, "mods") + " or read " + fixtureHome})
	if want := filepath.FromSlash("~/Games/friends"); e.Instance != want {
		t.Fatalf("instance = %q, want %q", e.Instance, want)
	}
	if want := "can't write " + filepath.FromSlash("~/Games/friends/mods") + " or read ~"; e.Msg != want {
		t.Fatalf("msg = %q, want %q", e.Msg, want)
	}
}

func TestRedactLeavesAPathThatOnlyStartsLikeHome(t *testing.T) {
	msg := "read " + fixtureHome + "-old/pack"
	if e := fixtureRedaction().Entry(Entry{Msg: msg}); e.Msg != msg {
		t.Fatalf("msg = %q", e.Msg)
	}
}

func TestRedactReachesIntoTheResultPayload(t *testing.T) {
	data, err := json.Marshal(map[string]any{
		"requires": map[string]any{"friends": map[string]any{"source": "https://tok@github.com/org/pack.git"}},
		"wrote":    []string{filepath.Join(fixtureHome, "Games", "friends", "mods", "a.jar")},
	})
	if err != nil {
		t.Fatal(err)
	}
	e := fixtureRedaction().Entry(Entry{Level: LevelInfo, Msg: "result", Data: data})
	var got struct {
		Requires map[string]struct{ Source string } `json:"requires"`
		Wrote    []string                           `json:"wrote"`
	}
	if err := json.Unmarshal(e.Data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Requires["friends"].Source != "https://github.com/org/pack.git" || got.Wrote[0] != filepath.FromSlash("~/Games/friends/mods/a.jar") {
		t.Fatalf("data = %s", e.Data)
	}
}

func TestRedactLeavesTheEntryItWasGiven(t *testing.T) {
	flags := map[string]string{"value": fixtureKey}
	in := Entry{Msg: fixtureKey, Flags: flags}
	fixtureRedaction().Entry(in)
	if in.Msg != fixtureKey || flags["value"] != fixtureKey {
		t.Fatalf("entry changed: %+v", in)
	}
}

func TestRedactWithNoHomeLeavesPaths(t *testing.T) {
	msg := "wrote " + filepath.FromSlash("/srv/games/mods")
	for _, home := range []string{"", string(filepath.Separator)} {
		if e := (Redaction{Home: home}).Entry(Entry{Msg: msg}); e.Msg != msg {
			t.Fatalf("home %q: msg = %q", home, e.Msg)
		}
	}
}
