package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/lock"
)

const (
	aliceUUID  = "11111111-1111-4111-8111-111111111111"
	bobUUID    = "22222222-2222-4222-8222-222222222222"
	malUUID    = "33333333-3333-4333-8333-333333333333"
	alice2UUID = "44444444-4444-4444-8444-444444444444"
)

func playerEntries(t *testing.T, path string) []map[string]any {
	t.Helper()
	var list []map[string]any
	if err := json.Unmarshal([]byte(readFile(t, path)), &list); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return list
}

func lockPlayers(t *testing.T, h *harness) []lock.Player {
	t.Helper()
	l, err := lock.Load(filepath.Join(h.dir, "shulker.lock"))
	if err != nil {
		t.Fatal(err)
	}
	return l.Players
}

func setPlayers(t *testing.T, h *harness, players map[string]any) {
	t.Helper()
	h.editManifest(t, func(m map[string]any) {
		m["server"].(map[string]any)["players"] = players
	})
}

func TestPlayersBuild(t *testing.T) {
	h := newHarness(t)
	h.mojang["Alice"] = aliceUUID
	h.mojang["Bob"] = bobUUID
	h.mojang["Mallory"] = malUUID
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack", "--target", "server")
	h.mustRun(t, "install")
	buildDir := filepath.Join(h.dir, "build", "server")
	whitelist := filepath.Join(buildDir, "whitelist.json")
	ops := filepath.Join(buildDir, "ops.json")
	bans := filepath.Join(buildDir, "banned-players.json")

	setPlayers(t, h, map[string]any{
		"whitelist": []any{map[string]any{"name": "Alice"}, map[string]any{"uuid": bobUUID}},
		"ops":       []any{map[string]any{"name": "Alice", "level": 3}},
		"bans":      []any{map[string]any{"name": "Mallory", "reason": "griefing", "expires": "2027-01-01T00:00:00Z"}},
	})
	h.mustRun(t, "build")
	entries := playerEntries(t, whitelist)
	if len(entries) != 2 || entries[0]["name"] != "Alice" || entries[0]["uuid"] != aliceUUID || entries[1]["name"] != "Bob" || entries[1]["uuid"] != bobUUID {
		t.Fatalf("whitelist: %v", entries)
	}
	entries = playerEntries(t, ops)
	if len(entries) != 1 || entries[0]["level"] != float64(3) || entries[0]["bypassesPlayerLimit"] != false || entries[0]["uuid"] != aliceUUID {
		t.Fatalf("ops: %v", entries)
	}
	entries = playerEntries(t, bans)
	created, _ := entries[0]["created"].(string)
	if len(entries) != 1 || entries[0]["reason"] != "griefing" || entries[0]["expires"] != "2027-01-01 00:00:00 +0000" || entries[0]["source"] != "shulker" || created == "" {
		t.Fatalf("bans: %v", entries)
	}
	locked := lockPlayers(t, h)
	if len(locked) != 3 || locked[0].Name != "Alice" || locked[1].Name != "Bob" || locked[2].UUID != malUUID || locked[0].ResolvedAt == "" {
		t.Fatalf("lock players: %+v", locked)
	}

	gameWritten := "[\n  {\n    \"uuid\": \"" + bobUUID + "\",\n    \"name\": \"Bob\"\n  },\n  {\n    \"uuid\": \"" + aliceUUID + "\",\n    \"name\": \"Alice\"\n  },\n  {\n    \"uuid\": \"55555555-5555-4555-8555-555555555555\",\n    \"name\": \"Carol\"\n  }\n]\n"
	if err := os.WriteFile(whitelist, []byte(gameWritten), 0o644); err != nil {
		t.Fatal(err)
	}
	hits := h.mojangHits
	stdout := h.mustRun(t, "install")
	if h.mojangHits != hits {
		t.Fatalf("install consulted Mojang: %s", stdout)
	}
	if got := readFile(t, whitelist); got != gameWritten {
		t.Fatalf("install rewrote an unchanged whitelist: %q", got)
	}

	h.editManifest(t, func(m map[string]any) {
		bans := m["server"].(map[string]any)["players"].(map[string]any)["bans"].([]any)
		bans[0].(map[string]any)["reason"] = "still griefing"
	})
	h.mustRun(t, "build")
	entries = playerEntries(t, bans)
	if entries[0]["reason"] != "still griefing" || entries[0]["created"] != created {
		t.Fatalf("ban rewrite: %v", entries)
	}

	gameBans := "[\n  {\n    \"uuid\": \"" + malUUID + "\",\n    \"name\": \"Mallory\",\n    \"created\": \"2026-09-10 10:36:33 -0500\",\n    \"source\": \"shulker\",\n    \"expires\": \"2026-12-31 18:00:00 -0600\",\n    \"reason\": \"still griefing\"\n  }\n]"
	if err := os.WriteFile(bans, []byte(gameBans), 0o644); err != nil {
		t.Fatal(err)
	}
	if stdout = h.mustRun(t, "build"); strings.Contains(stdout, "kept banned") || readFile(t, bans) != gameBans {
		t.Fatalf("local-time rewrite by the game should not count as drift: %s\n%s", stdout, readFile(t, bans))
	}

	delete(h.mojang, "Bob")
	h.mojang["Bobby"] = bobUUID
	_, stderr := h.mustRunStderr(t, "build")
	if !strings.Contains(stderr, "! player Bob is now named Bobby") {
		t.Fatalf("rename warning missing: %s", stderr)
	}
	entries = playerEntries(t, whitelist)
	if len(entries) != 3 || entries[0]["name"] != "Bobby" || entries[2]["name"] != "Carol" {
		t.Fatalf("whitelist after rename: %v", entries)
	}
	if locked = lockPlayers(t, h); locked[1].Name != "Bobby" || locked[1].UUID != bobUUID {
		t.Fatalf("lock after rename: %+v", locked)
	}

	h.mojang["Alice"] = alice2UUID
	code, stdout, _ := h.run(t, "--json", "build")
	if e := failureCode(t, stdout); code == 0 || e.Code != "player-reassigned" || len(e.Items) != 1 || !strings.Contains(e.Items[0], alice2UUID) {
		t.Fatalf("expected player-reassigned, got %d %s", code, stdout)
	}
	h.mustRun(t, "build", "--accept-player-change")
	entries = playerEntries(t, whitelist)
	if len(entries) != 3 || entries[0]["name"] != "Bobby" || entries[1]["name"] != "Carol" || entries[2]["uuid"] != alice2UUID {
		t.Fatalf("whitelist after reassignment: %v", entries)
	}
	if entries = playerEntries(t, ops); len(entries) != 1 || entries[0]["uuid"] != alice2UUID {
		t.Fatalf("ops after reassignment: %v", entries)
	}
	if locked = lockPlayers(t, h); len(locked) != 3 || locked[0].UUID != alice2UUID {
		t.Fatalf("lock after reassignment: %+v", locked)
	}

	setPlayers(t, h, map[string]any{
		"whitelist": []any{map[string]any{"name": "Alise"}},
	})
	code, stdout, _ = h.run(t, "--json", "build")
	if e := failureCode(t, stdout); code == 0 || e.Code != "player-unknown" || len(e.Items) != 1 || e.Items[0] != "Alise (did you mean Alice?)" {
		t.Fatalf("expected player-unknown, got %d %s", code, stdout)
	}

	setPlayers(t, h, map[string]any{
		"whitelist": []any{map[string]any{"name": "Alice"}},
	})
	h.mustRun(t, "build")
	if entries = playerEntries(t, whitelist); len(entries) != 2 || entries[0]["name"] != "Carol" || entries[1]["name"] != "Alice" {
		t.Fatalf("whitelist after removals: %v", entries)
	}
	if got := readFile(t, bans); got != "[]\n" {
		t.Fatalf("bans after removal: %q", got)
	}
	if got := readFile(t, ops); got != "[]\n" {
		t.Fatalf("ops after removal: %q", got)
	}
	if locked = lockPlayers(t, h); len(locked) != 1 || locked[0].UUID != alice2UUID {
		t.Fatalf("lock after removals: %+v", locked)
	}
}

func TestPlayerCommand(t *testing.T) {
	h := newHarness(t)
	h.mojang["Alice"] = aliceUUID
	h.mojang["Bob"] = bobUUID
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack", "--target", "server")
	h.mustRun(t, "install")
	setPlayers(t, h, map[string]any{"whitelist": []any{map[string]any{"name": "Alice"}, map[string]any{"name": "Bob"}}})
	h.mustRun(t, "build")

	stdout := h.mustRun(t, "player", "--all")
	if !strings.Contains(stdout, "✔ Alice "+aliceUUID) || !strings.Contains(stdout, "✔ Bob") {
		t.Fatalf("player: %s", stdout)
	}

	delete(h.mojang, "Bob")
	h.mojang["Bobby"] = bobUUID
	h.mojang["Alice"] = alice2UUID
	stdout = h.mustRun(t, "player", bobUUID, "Alice", "Nobody", "alise")
	for _, want := range []string{"~ Bobby " + bobUUID + " (renamed, was Bob)", "~ Alice " + alice2UUID + " (reassigned, was " + aliceUUID + ")", "- Nobody (unknown)\n", "- alise (unknown, did you mean Alice?)"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("player output missing %q:\n%s", want, stdout)
		}
	}
	locked := lockPlayers(t, h)
	if len(locked) != 2 || locked[0].Name != "Alice" || locked[0].UUID != aliceUUID || locked[1].Name != "Bobby" {
		t.Fatalf("player should record renames only: %+v", locked)
	}

	_, stdout, _ = h.run(t, "--json", "player", "Bobby")
	var env struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil || len(env.Data) != 1 || env.Data[0]["state"] != "ok" || env.Data[0]["uuid"] != bobUUID {
		t.Fatalf("player --json: %s (%v)", stdout, err)
	}

	code, stdout, _ := h.run(t, "--json", "player", "no spaces")
	if e := failureCode(t, stdout); code == 0 || e.Code != "player-invalid" {
		t.Fatalf("expected invalid-player, got %d %s", code, stdout)
	}

	for _, args := range [][]string{{"--json", "player"}, {"--json", "player", "--all", "Bobby"}} {
		code, stdout, _ := h.run(t, args...)
		if e := failureCode(t, stdout); code == 0 || e.Code != "usage" {
			t.Fatalf("%v: expected usage, got %d %s", args, code, stdout)
		}
	}
}
