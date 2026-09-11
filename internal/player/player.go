package player

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/near"
	"shulker.sh/shulker/internal/out"
)

const (
	DefaultAPIURL     = "https://api.mojang.com"
	DefaultSessionURL = "https://sessionserver.mojang.com"
	bulkLimit         = 10
)

var (
	namePattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,16}$`)
	uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{4}-?[0-9a-fA-F]{12}$`)
)

type Client struct {
	Fetch      *fetch.Client
	APIURL     string
	SessionURL string
	Now        func() time.Time
}

func New(f *fetch.Client) *Client {
	return &Client{Fetch: f, APIURL: DefaultAPIURL, SessionURL: DefaultSessionURL, Now: time.Now}
}

type Profile struct {
	Name string
	UUID string
}

type Ref struct {
	Name string
	UUID string
}

func ParseRef(s string) (Ref, error) {
	switch {
	case uuidPattern.MatchString(s):
		return Ref{UUID: Dashed(s)}, nil
	case namePattern.MatchString(s):
		return Ref{Name: s}, nil
	}
	return Ref{}, out.Errorf("invalid-player", "%q is neither a player name nor a uuid", s)
}

func (r Ref) String() string {
	if r.Name != "" {
		return r.Name
	}
	return r.UUID
}

func Dashed(id string) string {
	id = strings.ToLower(strings.ReplaceAll(id, "-", ""))
	if len(id) != 32 {
		return id
	}
	return id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
}

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

type Mode int

const (
	Recheck Mode = iota
	MissingOnly
)

type apiProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (c *Client) ByNames(ctx context.Context, names []string) (map[string]Profile, error) {
	found := map[string]Profile{}
	for start := 0; start < len(names); start += bulkLimit {
		end := min(start+bulkLimit, len(names))
		var profiles []apiProfile
		if err := c.Fetch.PostJSON(ctx, c.APIURL+"/profiles/minecraft", names[start:end], &profiles); err != nil {
			return nil, fmt.Errorf("mojang name lookup: %w", err)
		}
		for _, p := range profiles {
			found[strings.ToLower(p.Name)] = Profile{Name: p.Name, UUID: Dashed(p.ID)}
		}
	}
	return found, nil
}

func (c *Client) ByUUID(ctx context.Context, uuid string) (Profile, bool, error) {
	var p apiProfile
	url := c.SessionURL + "/session/minecraft/profile/" + strings.ReplaceAll(uuid, "-", "")
	ok, err := c.Fetch.GetJSONIfFound(ctx, url, &p)
	if err != nil {
		return Profile{}, false, fmt.Errorf("mojang profile lookup: %w", err)
	}
	if !ok {
		return Profile{}, false, nil
	}
	return Profile{Name: p.Name, UUID: Dashed(p.ID)}, true, nil
}

func Dedupe(refs []Ref) []Ref {
	seen := map[string]bool{}
	var unique []Ref
	for _, r := range refs {
		key := strings.ToLower(r.Name)
		if r.UUID != "" {
			key = Dashed(r.UUID)
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
		if ref.UUID != "" && p.UUID == Dashed(ref.UUID) {
			return p, true
		}
		if ref.UUID == "" && strings.EqualFold(p.Name, ref.Name) {
			return p, true
		}
	}
	return lock.Player{}, false
}

func (c *Client) Sync(ctx context.Context, refs []Ref, locked []lock.Player, mode Mode) ([]Result, error) {
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
	byName, err := c.ByNames(ctx, names)
	if err != nil {
		return nil, err
	}
	now := c.Now().UTC().Format(time.RFC3339)
	for _, i := range pending {
		ref := refs[i]
		var profile Profile
		var found bool
		if ref.UUID != "" {
			if profile, found, err = c.ByUUID(ctx, ref.UUID); err != nil {
				return nil, err
			}
		} else {
			profile, found = byName[strings.ToLower(ref.Name)]
		}
		results[i] = c.classify(ref, profile, found, locked, now)
	}
	return results, nil
}

func (c *Client) classify(ref Ref, profile Profile, found bool, locked []lock.Player, now string) Result {
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
		e := out.Errorf("player-unknown", "%d player(s) do not exist at Mojang", len(unknown))
		e.Candidates = unknown
		return warnings, e
	}
	if len(reassigned) > 0 {
		e := out.Errorf("player-reassigned", "%d player name(s) now belong to a different account; pass --accept-player-change to relock them", len(reassigned))
		e.Candidates = reassigned
		return warnings, e
	}
	return warnings, nil
}

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
