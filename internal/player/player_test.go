package player_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"shulker.sh/shulker/internal/env/envtest"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/player"
)

const (
	aliceUUID  = "11111111-1111-4111-8111-111111111111"
	bobUUID    = "22222222-2222-4222-8222-222222222222"
	alice2UUID = "44444444-4444-4444-8444-444444444444"
)

// newResolver is a resolver on the fake profile service, knowing Alice and Bob, with a fixed
// clock.
func newResolver(t *testing.T) (*player.Resolver, *envtest.Piston) {
	t.Helper()
	e := envtest.New(t)
	e.Piston.Profiles["Alice"] = aliceUUID
	e.Piston.Profiles["Bob"] = bobUUID
	e.Players.Now = func() time.Time { return time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC) }
	return e.Players, e.Piston
}

func sync(t *testing.T, r *player.Resolver, refs []player.Ref, locked []lock.Player, mode player.Mode) []player.Result {
	t.Helper()
	results, err := r.Sync(context.Background(), refs, locked, mode)
	if err != nil {
		t.Fatal(err)
	}
	return results
}

func TestParseRefTellsNamesFromUUIDs(t *testing.T) {
	if ref, err := player.ParseRef("Alice_1"); err != nil || ref != (player.Ref{Name: "Alice_1"}) || ref.String() != "Alice_1" {
		t.Fatalf("a name: %+v %v", ref, err)
	}
	undashed := "11111111111141118111111111111111"
	if ref, err := player.ParseRef(undashed); err != nil || ref != (player.Ref{UUID: aliceUUID}) || ref.String() != aliceUUID {
		t.Fatalf("an undashed uuid is dashed: %+v %v", ref, err)
	}
	for _, bad := range []string{"no spaces", "ab", "seventeen_letters_", "1111-not-a-uuid"} {
		if _, err := player.ParseRef(bad); out.CodeOf(err) != "player-invalid" {
			t.Fatalf("%q: %v", bad, err)
		}
	}
	if !player.IsName("Steve") || player.IsName("Steve!") || !player.IsUUID(aliceUUID) || player.IsUUID("Steve") {
		t.Fatal("IsName and IsUUID should agree with ParseRef")
	}
}

func TestDedupeDropsLaterRefsToOnePlayer(t *testing.T) {
	refs := []player.Ref{{Name: "Alice"}, {Name: "alice"}, {UUID: bobUUID}, {UUID: "22222222222242228222222222222222"}, {Name: "Bob"}}
	got := player.Dedupe(refs)
	want := []player.Ref{{Name: "Alice"}, {UUID: bobUUID}, {Name: "Bob"}}
	if !slices.Equal(got, want) {
		t.Fatalf("dedupe: %v", got)
	}
}

func TestRefsOfListsEveryServerPlayer(t *testing.T) {
	m := &manifest.Manifest{Server: &manifest.Server{Players: &manifest.Players{
		Whitelist: []manifest.Player{{Name: "Alice"}},
		Ops:       []manifest.Op{{Player: manifest.Player{UUID: bobUUID}}},
		Bans:      []manifest.Ban{{Player: manifest.Player{Name: "Mallory"}}},
	}}}
	want := []player.Ref{{Name: "Alice"}, {UUID: bobUUID}, {Name: "Mallory"}}
	if got := player.RefsOf(m); !slices.Equal(got, want) {
		t.Fatalf("refs: %v", got)
	}
	if got := player.RefsOf(&manifest.Manifest{}); len(got) != 0 {
		t.Fatalf("a client-only manifest names nobody: %v", got)
	}
}

func TestFindMatchesByUUIDOrCaselessName(t *testing.T) {
	locked := []lock.Player{{Name: "Alice", UUID: aliceUUID}}
	if p, ok := player.Find(locked, player.Ref{Name: "ALICE"}); !ok || p.UUID != aliceUUID {
		t.Fatalf("by name: %+v %v", p, ok)
	}
	if p, ok := player.Find(locked, player.Ref{UUID: "11111111111141118111111111111111"}); !ok || p.Name != "Alice" {
		t.Fatalf("by undashed uuid: %+v %v", p, ok)
	}
	if _, ok := player.Find(locked, player.Ref{Name: "Alice", UUID: bobUUID}); ok {
		t.Fatal("a ref with a uuid matches on the uuid alone")
	}
}

func TestSyncResolvesNamesAndUUIDsAgainstMojang(t *testing.T) {
	r, piston := newResolver(t)
	results := sync(t, r, []player.Ref{{Name: "alice"}, {UUID: bobUUID}, {Name: "Nobody"}}, nil, player.Recheck)
	if len(results) != 3 {
		t.Fatalf("results: %+v", results)
	}
	if a := results[0]; a.Input != "alice" || a.Name != "Alice" || a.UUID != aliceUUID || a.State != player.Ok || a.ResolvedAt != "2026-09-26T12:00:00Z" {
		t.Fatalf("a name resolves to Mojang's casing and uuid: %+v", a)
	}
	if b := results[1]; b.Input != bobUUID || b.Name != "Bob" || b.UUID != bobUUID || b.State != player.Ok {
		t.Fatalf("a uuid resolves to its name: %+v", b)
	}
	if n := results[2]; n.Input != "Nobody" || n.State != player.Unknown || n.Name != "Nobody" || n.UUID != "" || len(n.Candidates) != 0 {
		t.Fatalf("a name Mojang lacks is unknown: %+v", n)
	}
	if piston.ProfileHits.Load() != 2 {
		t.Fatalf("one bulk name lookup and one uuid lookup: %d", piston.ProfileHits.Load())
	}
}

func TestSyncMissingOnlyLeavesLockedPlayersAlone(t *testing.T) {
	r, piston := newResolver(t)
	locked := []lock.Player{{Name: "Alice", UUID: aliceUUID, ResolvedAt: "2026-01-01T00:00:00Z"}}
	results := sync(t, r, []player.Ref{{Name: "Alice"}}, locked, player.MissingOnly)
	if results[0].State != player.Ok || results[0].ResolvedAt != "2026-01-01T00:00:00Z" || piston.ProfileHits.Load() != 0 {
		t.Fatalf("a locked player is answered from the lock: %+v (%d hits)", results[0], piston.ProfileHits.Load())
	}

	results = sync(t, r, []player.Ref{{Name: "Alice"}, {Name: "Bob"}}, locked, player.MissingOnly)
	if results[1].UUID != bobUUID || results[1].ResolvedAt != "2026-09-26T12:00:00Z" || piston.ProfileHits.Load() != 1 {
		t.Fatalf("only the missing player is looked up: %+v (%d hits)", results, piston.ProfileHits.Load())
	}
}

func TestSyncRecheckKeepsTheStampOfAnUnchangedPlayer(t *testing.T) {
	r, _ := newResolver(t)
	locked := []lock.Player{{Name: "Alice", UUID: aliceUUID, ResolvedAt: "2026-01-01T00:00:00Z"}}
	results := sync(t, r, []player.Ref{{Name: "Alice"}}, locked, player.Recheck)
	if results[0].State != player.Ok || results[0].ResolvedAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("a player Mojang still agrees on keeps its stamp: %+v", results[0])
	}
}

func TestSyncTellsARenameFromAReassignment(t *testing.T) {
	r, piston := newResolver(t)
	delete(piston.Profiles, "Bob")
	piston.Profiles["Bobby"] = bobUUID
	piston.Profiles["Alice"] = alice2UUID
	locked := []lock.Player{{Name: "Alice", UUID: aliceUUID}, {Name: "Bob", UUID: bobUUID}}

	results := sync(t, r, []player.Ref{{UUID: bobUUID}, {Name: "Alice"}, {Name: "alise"}}, locked, player.Recheck)
	if b := results[0]; b.State != player.Renamed || b.Name != "Bobby" || b.Previous != "Bob" || b.ResolvedAt != "2026-09-26T12:00:00Z" {
		t.Fatalf("the same uuid under a new name is a rename: %+v", b)
	}
	if a := results[1]; a.State != player.Reassigned || a.UUID != alice2UUID || a.Previous != aliceUUID {
		t.Fatalf("the same name on a new uuid is a reassignment: %+v", a)
	}
	if u := results[2]; u.State != player.Unknown || !slices.Equal(u.Candidates, []string{"Alice"}) {
		t.Fatalf("an unknown name suggests the locked one it is near: %+v", u)
	}
}

func TestPolicyWarnsOnRenamesAndFailsOnTheRest(t *testing.T) {
	renamed := player.Result{Input: bobUUID, Name: "Bobby", UUID: bobUUID, State: player.Renamed, Previous: "Bob"}
	reassigned := player.Result{Input: "Alice", Name: "Alice", UUID: alice2UUID, State: player.Reassigned, Previous: aliceUUID}
	unknown := player.Result{Input: "alise", Name: "alise", State: player.Unknown, Candidates: []string{"Alice"}}

	warnings, err := player.Policy([]player.Result{renamed}, false)
	if err != nil || len(warnings) != 1 || warnings[0] != "player Bob is now named Bobby ("+bobUUID+")." {
		t.Fatalf("a rename warns: %v %v", warnings, err)
	}

	_, err = player.Policy([]player.Result{renamed, reassigned}, false)
	e := out.AsError(err)
	if e.Code != "player-reassigned" || len(e.Items) != 1 || e.Items[0] != "Alice: the lock has "+aliceUUID+", Mojang now reports "+alice2UUID || e.Help == "" {
		t.Fatalf("a reassignment fails until accepted: %+v", e)
	}
	warnings, err = player.Policy([]player.Result{reassigned}, true)
	if err != nil || len(warnings) != 1 || warnings[0] != "player Alice is now a different account: "+alice2UUID+" was "+aliceUUID+"." {
		t.Fatalf("an accepted reassignment warns: %v %v", warnings, err)
	}

	_, err = player.Policy([]player.Result{reassigned, unknown}, true)
	e = out.AsError(err)
	if e.Code != "player-unknown" || len(e.Items) != 1 || e.Items[0] != "alise (did you mean Alice?)" {
		t.Fatalf("an unknown player fails first: %+v", e)
	}
}

func TestApplyIsTheLocksNewPlayerList(t *testing.T) {
	locked := []lock.Player{{Name: "Alice", UUID: aliceUUID, ResolvedAt: "old"}, {Name: "Gone", UUID: "99999999-9999-4999-8999-999999999999"}}
	results := []player.Result{
		{Name: "Bobby", UUID: bobUUID, State: player.Renamed, Previous: "Bob", ResolvedAt: "now"},
		{Name: "Alice", UUID: alice2UUID, State: player.Reassigned, Previous: aliceUUID, ResolvedAt: "now"},
		{Input: "alise", State: player.Unknown},
	}
	got := player.Apply(results, locked, false)
	want := []lock.Player{{Name: "Alice", UUID: aliceUUID, ResolvedAt: "old"}, {Name: "Bobby", UUID: bobUUID, ResolvedAt: "now"}}
	if !slices.Equal(got, want) {
		t.Fatalf("a refused reassignment keeps the locked account, and a player no longer named is dropped: %+v", got)
	}
	got = player.Apply(results, locked, true)
	want = []lock.Player{{Name: "Alice", UUID: alice2UUID, ResolvedAt: "now"}, {Name: "Bobby", UUID: bobUUID, ResolvedAt: "now"}}
	if !slices.Equal(got, want) {
		t.Fatalf("an accepted reassignment relocks the name: %+v", got)
	}
}

func TestUpdateRenamesLockedPlayersAndAddsNone(t *testing.T) {
	locked := []lock.Player{{Name: "Alice", UUID: aliceUUID, ResolvedAt: "old"}, {Name: "Bob", UUID: bobUUID, ResolvedAt: "old"}}
	results := []player.Result{
		{Name: "Bobby", UUID: bobUUID, State: player.Renamed, Previous: "Bob", ResolvedAt: "now"},
		{Name: "Alice", UUID: alice2UUID, State: player.Reassigned, Previous: aliceUUID, ResolvedAt: "now"},
		{Name: "Carol", UUID: "55555555-5555-4555-8555-555555555555", State: player.Ok, ResolvedAt: "now"},
	}
	got := player.Update(results, locked)
	want := []lock.Player{{Name: "Alice", UUID: aliceUUID, ResolvedAt: "old"}, {Name: "Bobby", UUID: bobUUID, ResolvedAt: "now"}}
	if !slices.Equal(got, want) {
		t.Fatalf("update: %+v", got)
	}
}
