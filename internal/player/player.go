// Package player resolves the player names and uuids a manifest lists, through Mojang's profiles,
// and decides what the lock records for them.
package player

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/mojang"
	"shulker.sh/shulker/internal/near"
	"shulker.sh/shulker/internal/out"
)

var (
	namePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,16}$`)
	uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{12}$`)
)

// Resolver looks players up through Mojang's profiles and stamps each answer with its clock.
type Resolver struct {
	Profiles *mojang.Profiles
	Now      func() time.Time
}

func NewResolver(profiles *mojang.Profiles) *Resolver {
	return &Resolver{Profiles: profiles, Now: time.Now}
}

type Ref struct {
	Name string
	UUID string
}

// IsName reports whether a string is a Minecraft username: three to sixteen letters, digits or
// underscores, which is what an offline account's name is held to as well.
func IsName(s string) bool { return namePattern.MatchString(s) }

// IsUUID reports whether a string is a player UUID, dashed or not.
func IsUUID(s string) bool { return uuidPattern.MatchString(s) }

func ParseRef(s string) (Ref, error) {
	switch {
	case IsUUID(s):
		return Ref{UUID: mojang.Dashed(s)}, nil
	case IsName(s):
		return Ref{Name: s}, nil
	}
	return Ref{}, out.Errorf("player-invalid", "%q is neither a player name nor a uuid", s)
}

func (r Ref) String() string {
	if r.Name != "" {
		return r.Name
	}
	return r.UUID
}

// State is how a player compares with the lock: Ok, Renamed (same uuid, new name), Reassigned
// (same name, another uuid) or Unknown to Mojang.
type State string

const (
	Ok         State = "ok"
	Renamed    State = "renamed"
	Reassigned State = "reassigned"
	Unknown    State = "unknown"
)

type Result struct {
	Input      string   `json:"input"`
	Name       string   `json:"name,omitempty"`
	UUID       string   `json:"uuid,omitempty"`
	State      State    `json:"state"`
	Previous   string   `json:"previous,omitempty"`
	Candidates []string `json:"candidates,omitempty"`
	ResolvedAt string   `json:"resolvedAt,omitempty"`
}

// Mode is which players Sync asks Mojang about: all of them, or only those the lock lacks.
type Mode int

const (
	Recheck Mode = iota
	MissingOnly
)

// Dedupe drops later refs to a player already listed: by uuid when a ref has one, else by name
// in any case.
func Dedupe(refs []Ref) []Ref {
	seen := map[string]bool{}
	var unique []Ref
	for _, r := range refs {
		key := strings.ToLower(r.Name)
		if r.UUID != "" {
			key = mojang.Dashed(r.UUID)
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, r)
	}
	return unique
}

func Find(locked []lock.Player, ref Ref) (lock.Player, bool) {
	for _, p := range locked {
		if ref.UUID != "" && p.UUID == mojang.Dashed(ref.UUID) {
			return p, true
		}
		if ref.UUID == "" && strings.EqualFold(p.Name, ref.Name) {
			return p, true
		}
	}
	return lock.Player{}, false
}

// Sync resolves refs against Mojang and classifies each against the lock.
func (r *Resolver) Sync(ctx context.Context, refs []Ref, locked []lock.Player, mode Mode) ([]Result, error) {
	refs = Dedupe(refs)
	results := make([]Result, len(refs))
	var pending []int
	for i, ref := range refs {
		results[i] = Result{Input: ref.String()}
		if mode == MissingOnly {
			if p, ok := Find(locked, ref); ok {
				results[i].Name, results[i].UUID, results[i].State, results[i].ResolvedAt = p.Name, p.UUID, Ok, p.ResolvedAt
				continue
			}
		}
		pending = append(pending, i)
	}
	if len(pending) == 0 {
		return results, nil
	}
	var names []string
	for _, i := range pending {
		if refs[i].UUID == "" {
			names = append(names, refs[i].Name)
		}
	}
	byName, err := r.Profiles.ByNames(ctx, names)
	if err != nil {
		return nil, err
	}
	now := r.Now().UTC().Format(time.RFC3339)
	for _, i := range pending {
		ref := refs[i]
		var profile mojang.Profile
		var found bool
		if ref.UUID != "" {
			if profile, found, err = r.Profiles.ByUUID(ctx, ref.UUID); err != nil {
				return nil, err
			}
		} else {
			profile, found = byName[strings.ToLower(ref.Name)]
		}
		results[i] = classify(ref, profile, found, locked, now)
	}
	return results, nil
}

func classify(ref Ref, profile mojang.Profile, found bool, locked []lock.Player, now string) Result {
	r := Result{Input: ref.String(), ResolvedAt: now}
	if !found {
		r.State = Unknown
		r.Name, r.UUID = ref.Name, ref.UUID
		if ref.Name != "" {
			r.Candidates = near.Closest(ref.Name, lockedNames(locked), 3)
		}
		return r
	}
	r.Name, r.UUID, r.State = profile.Name, profile.UUID, Ok
	if prev, ok := Find(locked, Ref{UUID: profile.UUID}); ok {
		if prev.Name != profile.Name {
			r.State, r.Previous = Renamed, prev.Name
		} else {
			r.ResolvedAt = prev.ResolvedAt
		}
		return r
	}
	if ref.Name != "" {
		if prev, ok := Find(locked, Ref{Name: ref.Name}); ok && prev.UUID != profile.UUID {
			r.State, r.Previous = Reassigned, prev.UUID
		}
	}
	return r
}

func lockedNames(locked []lock.Player) []string {
	names := make([]string, 0, len(locked))
	for _, p := range locked {
		names = append(names, p.Name)
	}
	return names
}

// Policy turns renames, and reassignments the user accepted, into warnings. It fails on an
// unknown player, and on a reassignment the user didn't accept.
func Policy(results []Result, acceptChange bool) ([]string, error) {
	var warnings, reassigned, unknown []string
	for _, r := range results {
		switch r.State {
		case Renamed:
			warnings = append(warnings, fmt.Sprintf("player %s is now named %s (%s)", r.Previous, r.Name, r.UUID))
		case Reassigned:
			if acceptChange {
				warnings = append(warnings, fmt.Sprintf("player %s is now a different account: %s was %s", r.Name, r.UUID, r.Previous))
				continue
			}
			reassigned = append(reassigned, fmt.Sprintf("%s: the lock has %s, Mojang now reports %s", r.Name, r.Previous, r.UUID))
		case Unknown:
			line := r.Input
			if len(r.Candidates) > 0 {
				line += " (did you mean " + strings.Join(r.Candidates, ", ") + "?)"
			}
			unknown = append(unknown, line)
		}
	}
	if len(unknown) > 0 {
		e := out.Errorf("player-unknown", "%s at Mojang", out.Count(len(unknown), "player doesn't exist", "players don't exist"))
		e.Items = unknown
		return warnings, e
	}
	if len(reassigned) > 0 {
		e := out.Errorf("player-reassigned", "%s to a different account", out.Count(len(reassigned), "player name now belongs", "player names now belong"))
		e.Help = "pass `--accept-player-change` to relock them"
		e.Items = reassigned
		return warnings, e
	}
	return warnings, nil
}

// Apply is the lock's new player list: the players results name, with a reassigned name kept on
// its locked account unless the user accepted the change.
func Apply(results []Result, locked []lock.Player, acceptChange bool) []lock.Player {
	byUUID := map[string]lock.Player{}
	for _, r := range results {
		switch r.State {
		case Ok, Renamed:
			byUUID[r.UUID] = lock.Player{Name: r.Name, UUID: r.UUID, ResolvedAt: r.ResolvedAt}
		case Reassigned:
			if acceptChange {
				byUUID[r.UUID] = lock.Player{Name: r.Name, UUID: r.UUID, ResolvedAt: r.ResolvedAt}
			} else if prev, ok := Find(locked, Ref{Name: r.Name}); ok {
				byUUID[prev.UUID] = prev
			}
		}
	}
	return sorted(byUUID)
}

// Update refreshes the names of players already locked, adding and dropping none.
func Update(results []Result, locked []lock.Player) []lock.Player {
	byUUID := map[string]lock.Player{}
	for _, p := range locked {
		byUUID[p.UUID] = p
	}
	for _, r := range results {
		if _, ok := byUUID[r.UUID]; ok && (r.State == Ok || r.State == Renamed) {
			byUUID[r.UUID] = lock.Player{Name: r.Name, UUID: r.UUID, ResolvedAt: r.ResolvedAt}
		}
	}
	return sorted(byUUID)
}

func sorted(byUUID map[string]lock.Player) []lock.Player {
	players := make([]lock.Player, 0, len(byUUID))
	for _, p := range byUUID {
		players = append(players, p)
	}
	sort.Slice(players, func(i, j int) bool {
		if a, b := strings.ToLower(players[i].Name), strings.ToLower(players[j].Name); a != b {
			return a < b
		}
		return players[i].UUID < players[j].UUID
	})
	return players
}
