package sync

import (
	"context"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/player"
	"shulker.sh/shulker/internal/project"
)

const (
	aliceUUID  = "11111111-1111-4111-8111-111111111111"
	bobUUID    = "22222222-2222-4222-8222-222222222222"
	alice2UUID = "44444444-4444-4444-8444-444444444444"
)

// playersHarness is the harness with a server side whitelisting Alice by name and Bob by uuid, both known to
// the fake profile service.
func playersHarness(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.env.Piston.Profiles["Alice"] = aliceUUID
	h.env.Piston.Profiles["Bob"] = bobUUID
	h.editManifest(func(m *manifest.Manifest) {
		m.Server = &manifest.Server{Players: &manifest.Players{Whitelist: []manifest.Player{{Name: "Alice"}, {UUID: bobUUID}}}}
	})
	return h
}

func (h *harness) players(p *project.Project, mode player.Mode, acceptChange, persist bool) error {
	h.t.Helper()
	return Players(context.Background(), h.e, p, mode, acceptChange, persist)
}

func names(players []lock.Player) []string {
	var names []string
	for _, p := range players {
		names = append(names, p.Name+" "+p.UUID)
	}
	return names
}

func TestPlayersLocksTheManifestsPlayers(t *testing.T) {
	h := playersHarness(t)
	p := h.project()
	if err := h.players(p, player.MissingOnly, false, true); err != nil {
		t.Fatal(err)
	}
	want := []string{"Alice " + aliceUUID, "Bob " + bobUUID}
	if got := names(p.Lock.Players); len(got) != 2 || got[0] != want[0] || got[1] != want[1] || p.Lock.Players[0].ResolvedAt == "" {
		t.Fatalf("locked players: %+v", p.Lock.Players)
	}
	if got := names(h.project().Lock.Players); len(got) != 2 {
		t.Fatalf("the lock should be saved: %v", got)
	}

	hits := h.env.Piston.ProfileHits.Load()
	if err := h.players(h.project(), player.MissingOnly, false, true); err != nil {
		t.Fatal(err)
	}
	if h.env.Piston.ProfileHits.Load() != hits {
		t.Fatal("players already locked are not looked up again")
	}
}

func TestPlayersLeavesTheLockOnDiskWithoutPersist(t *testing.T) {
	h := playersHarness(t)
	p := h.project()
	if err := h.players(p, player.MissingOnly, false, false); err != nil {
		t.Fatal(err)
	}
	if len(p.Lock.Players) != 2 || len(h.project().Lock.Players) != 0 {
		t.Fatalf("the change stays in memory: %d in memory, %d on disk", len(p.Lock.Players), len(h.project().Lock.Players))
	}
}

func TestPlayersWarnsOnARenameAndRefusesAReassignment(t *testing.T) {
	h := playersHarness(t)
	if err := h.players(h.project(), player.MissingOnly, false, true); err != nil {
		t.Fatal(err)
	}
	delete(h.env.Piston.Profiles, "Bob")
	h.env.Piston.Profiles["Bobby"] = bobUUID
	h.env.Piston.Profiles["Alice"] = alice2UUID

	p := h.project()
	err := h.players(p, player.Recheck, false, true)
	e := out.AsError(err)
	if e.Code != "player-reassigned" || len(e.Items) != 1 || e.Items[0] != "Alice: the lock has "+aliceUUID+", Mojang now reports "+alice2UUID {
		t.Fatalf("a reassignment refuses: %+v", e)
	}
	if !h.warned("player Bob is now named Bobby") {
		t.Fatalf("the rename should warn first: %v", h.env.Warnings)
	}
	if got := names(h.project().Lock.Players); got[0] != "Alice "+aliceUUID || got[1] != "Bob "+bobUUID {
		t.Fatalf("a refused sync changes nothing on disk: %v", got)
	}

	p = h.project()
	if err := h.players(p, player.Recheck, true, true); err != nil {
		t.Fatal(err)
	}
	if !h.warned("player Alice is now a different account") {
		t.Fatalf("an accepted reassignment warns: %v", h.env.Warnings)
	}
	if got := names(h.project().Lock.Players); got[0] != "Alice "+alice2UUID || got[1] != "Bobby "+bobUUID {
		t.Fatalf("the lock follows the rename and the accepted reassignment: %v", got)
	}
}

func TestPlayersRefusesAnUnknownPlayerWithACandidate(t *testing.T) {
	h := playersHarness(t)
	if err := h.players(h.project(), player.MissingOnly, false, true); err != nil {
		t.Fatal(err)
	}
	h.editManifest(func(m *manifest.Manifest) {
		m.Server.Players.Whitelist = []manifest.Player{{Name: "Alise"}}
	})
	err := h.players(h.project(), player.MissingOnly, false, true)
	e := out.AsError(err)
	if e.Code != "player-unknown" || len(e.Items) != 1 || e.Items[0] != "Alise (did you mean Alice?)" {
		t.Fatalf("an unknown player: %+v", e)
	}
}

func TestPlayersDropsWhoTheManifestNoLongerNames(t *testing.T) {
	h := playersHarness(t)
	if err := h.players(h.project(), player.MissingOnly, false, true); err != nil {
		t.Fatal(err)
	}
	h.editManifest(func(m *manifest.Manifest) {
		m.Server.Players.Whitelist = []manifest.Player{{Name: "Alice"}}
	})
	if err := h.players(h.project(), player.MissingOnly, false, true); err != nil {
		t.Fatal(err)
	}
	if got := names(h.project().Lock.Players); len(got) != 1 || got[0] != "Alice "+aliceUUID {
		t.Fatalf("locked players after a removal: %v", got)
	}
}
