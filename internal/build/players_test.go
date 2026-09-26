package build

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

const (
	aliceUUID = "11111111-1111-4111-8111-111111111111"
	bobUUID   = "22222222-2222-4222-8222-222222222222"
	malUUID   = "33333333-3333-4333-8333-333333333333"
	carolUUID = "55555555-5555-4555-8555-555555555555"
)

// playersProject is a server project whose lock knows Alice, Bob and Mallory, with Alice and
// Bob whitelisted, Alice an op and Mallory banned.
func playersProject(t *testing.T) *testProject {
	t.Helper()
	p := newProject(t)
	p.b.Lock.Players = []lock.Player{
		{Name: "Alice", UUID: aliceUUID, ResolvedAt: "2026-09-26T12:00:00Z"},
		{Name: "Bob", UUID: bobUUID, ResolvedAt: "2026-09-26T12:00:00Z"},
		{Name: "Mallory", UUID: malUUID, ResolvedAt: "2026-09-26T12:00:00Z"},
	}
	p.b.Manifest.Server.Players = &manifest.Players{
		Whitelist: []manifest.Player{{Name: "Alice"}, {UUID: bobUUID}},
		Ops:       []manifest.Op{{Player: manifest.Player{Name: "Alice"}, Level: 3}},
		Bans:      []manifest.Ban{{Player: manifest.Player{Name: "Mallory"}, Reason: "griefing", Expires: "2027-01-01T00:00:00Z"}},
	}
	return p
}

func (p *testProject) playerEntries(rel string) []map[string]any {
	p.t.Helper()
	var list []map[string]any
	if err := json.Unmarshal([]byte(p.built("server", rel)), &list); err != nil {
		p.t.Fatalf("%s: %v", rel, err)
	}
	return list
}

func TestBuildWritesThePlayerFilesFromTheLock(t *testing.T) {
	p := playersProject(t)
	p.mustBuild("server", Options{NoLauncher: true})

	entries := p.playerEntries(WhitelistFile)
	if len(entries) != 2 || entries[0]["name"] != "Alice" || entries[0]["uuid"] != aliceUUID || entries[1]["name"] != "Bob" || entries[1]["uuid"] != bobUUID {
		t.Fatalf("whitelist: %v", entries)
	}
	entries = p.playerEntries(OpsFile)
	if len(entries) != 1 || entries[0]["level"] != float64(3) || entries[0]["bypassesPlayerLimit"] != false || entries[0]["uuid"] != aliceUUID {
		t.Fatalf("ops: %v", entries)
	}
	entries = p.playerEntries(BansFile)
	created, _ := entries[0]["created"].(string)
	if len(entries) != 1 || entries[0]["reason"] != "griefing" || entries[0]["expires"] != "2027-01-01 00:00:00 +0000" || entries[0]["source"] != "shulker" || created == "" {
		t.Fatalf("bans: %v", entries)
	}
}

func TestBuildDefaultsAnOpsLevelAndABansReason(t *testing.T) {
	p := playersProject(t)
	p.b.Manifest.Server.Players.Ops = []manifest.Op{{Player: manifest.Player{Name: "Bob"}}}
	p.b.Manifest.Server.Players.Bans = []manifest.Ban{{Player: manifest.Player{Name: "Mallory"}}}
	p.mustBuild("server", Options{NoLauncher: true})

	if entries := p.playerEntries(OpsFile); entries[0]["level"] != float64(4) {
		t.Fatalf("an op is level 4 by default: %v", entries)
	}
	if entries := p.playerEntries(BansFile); entries[0]["expires"] != "forever" || entries[0]["reason"] != "Banned by an operator." {
		t.Fatalf("a ban is forever, by an operator, by default: %v", entries)
	}
}

func TestBuildRefusesAPlayerTheLockLacks(t *testing.T) {
	p := playersProject(t)
	p.b.Manifest.Server.Players.Whitelist = append(p.b.Manifest.Server.Players.Whitelist, manifest.Player{Name: "Carol"})
	_, err := p.build("server", Options{NoLauncher: true})
	if out.CodeOf(err) != "player-unresolved" {
		t.Fatalf("a player not in the lock: %v", err)
	}
}

// The game rewrites its player files in its own order and adds who it lets in, and neither is
// drift: the build leaves the file as it is when every entry it owns is still there.
func TestBuildLeavesAPlayerFileTheGameReorderedAlone(t *testing.T) {
	p := playersProject(t)
	p.mustBuild("server", Options{NoLauncher: true})
	gameWritten := "[\n  {\n    \"uuid\": \"" + bobUUID + "\",\n    \"name\": \"Bob\"\n  },\n  {\n    \"uuid\": \"" + aliceUUID + "\",\n    \"name\": \"Alice\"\n  },\n  {\n    \"uuid\": \"" + carolUUID + "\",\n    \"name\": \"Carol\"\n  }\n]\n"
	p.writeBuilt("server", WhitelistFile, gameWritten)

	report := p.mustBuild("server", Options{NoLauncher: true})
	if slices.Contains(report.Written, WhitelistFile) || p.built("server", WhitelistFile) != gameWritten {
		t.Fatalf("a reordered whitelist is not drift: %v\n%s", report.Written, p.built("server", WhitelistFile))
	}
}

func TestBuildKeepsABansCreatedStampAcrossARewrite(t *testing.T) {
	p := playersProject(t)
	p.mustBuild("server", Options{NoLauncher: true})
	created := p.playerEntries(BansFile)[0]["created"]

	p.b.Manifest.Server.Players.Bans[0].Reason = "still griefing"
	p.mustBuild("server", Options{NoLauncher: true})
	entries := p.playerEntries(BansFile)
	if entries[0]["reason"] != "still griefing" || entries[0]["created"] != created {
		t.Fatalf("ban rewrite: %v", entries)
	}

	gameBans := "[\n  {\n    \"uuid\": \"" + malUUID + "\",\n    \"name\": \"Mallory\",\n    \"created\": \"2026-09-10 10:36:33 -0500\",\n    \"source\": \"shulker\",\n    \"expires\": \"2026-12-31 18:00:00 -0600\",\n    \"reason\": \"still griefing\"\n  }\n]"
	p.writeBuilt("server", BansFile, gameBans)
	report := p.mustBuild("server", Options{NoLauncher: true})
	if slices.ContainsFunc(report.Kept, func(k string) bool { return strings.HasPrefix(k, BansFile) }) || p.built("server", BansFile) != gameBans {
		t.Fatalf("a local-time rewrite by the game is not drift: kept %v\n%s", report.Kept, p.built("server", BansFile))
	}
}

func TestBuildFollowsTheLocksRenamesAndRemovals(t *testing.T) {
	p := playersProject(t)
	p.mustBuild("server", Options{NoLauncher: true})
	p.writeBuilt("server", WhitelistFile, "[\n  {\n    \"uuid\": \""+carolUUID+"\",\n    \"name\": \"Carol\"\n  },\n  {\n    \"uuid\": \""+aliceUUID+"\",\n    \"name\": \"Alice\"\n  },\n  {\n    \"uuid\": \""+bobUUID+"\",\n    \"name\": \"Bob\"\n  }\n]\n")

	p.b.Lock.Players[1].Name = "Bobby"
	p.mustBuild("server", Options{NoLauncher: true})
	entries := p.playerEntries(WhitelistFile)
	if len(entries) != 3 || entries[0]["name"] != "Carol" || entries[2]["name"] != "Bobby" {
		t.Fatalf("whitelist after a rename: %v", entries)
	}

	p.b.Manifest.Server.Players = &manifest.Players{Whitelist: []manifest.Player{{Name: "Alice"}}}
	p.mustBuild("server", Options{NoLauncher: true})
	if entries = p.playerEntries(WhitelistFile); len(entries) != 2 || entries[0]["name"] != "Carol" || entries[1]["name"] != "Alice" {
		t.Fatalf("whitelist after removals keeps who the game added: %v", entries)
	}
	if p.built("server", BansFile) != "[]\n" || p.built("server", OpsFile) != "[]\n" {
		t.Fatalf("emptied lists: bans %q ops %q", p.built("server", BansFile), p.built("server", OpsFile))
	}
}
