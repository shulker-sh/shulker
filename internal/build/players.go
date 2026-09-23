package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/player"
)

const (
	WhitelistFile = "whitelist.json"
	OpsFile       = "ops.json"
	BansFile      = "banned-players.json"
	banTimeLayout = "2006-01-02 15:04:05 -0700"
)

type playerEntry map[string]any

type playerFile struct {
	order     []string
	entries   map[string]playerEntry
	isBanList bool
	now       func() time.Time
}

func newPlayerFile(bans bool) *playerFile {
	return &playerFile{entries: map[string]playerEntry{}, isBanList: bans, now: time.Now}
}

func (f *playerFile) add(uuid string, e playerEntry) {
	if _, dup := f.entries[uuid]; !dup {
		f.order = append(f.order, uuid)
	}
	f.entries[uuid] = e
}

func (f *playerFile) keys() []string {
	keys := append([]string{}, f.order...)
	sort.Strings(keys)
	return keys
}

func (f *playerFile) values() map[string]string {
	values := map[string]string{}
	for u, e := range f.entries {
		data, _ := json.Marshal(driftFields(e))
		values[u] = string(data)
	}
	return values
}

func (f *playerFile) existingValues(existing []byte) map[string]string {
	values := map[string]string{}
	list, err := parsePlayerEntries(existing)
	if err != nil {
		return values
	}
	for _, e := range list {
		data, _ := json.Marshal(driftFields(e))
		values[entryUUID(e)] = string(data)
	}
	return values
}

func (f *playerFile) render(existing []byte, kept, dropped map[string]bool) ([]byte, error) {
	list, err := parsePlayerEntries(existing)
	if err != nil {
		return nil, err
	}
	merged := []playerEntry{}
	seen := map[string]bool{}
	for _, e := range list {
		u := entryUUID(e)
		if dropped[u] {
			continue
		}
		own, ok := f.entries[u]
		if !ok || kept[u] {
			merged = append(merged, e)
			if ok {
				seen[u] = true
			}
			continue
		}
		seen[u] = true
		merged = append(merged, f.stamped(own, e))
	}
	for _, u := range f.order {
		if !seen[u] {
			merged = append(merged, f.stamped(f.entries[u], nil))
		}
	}
	return fsutil.MarshalJSON(merged)
}

func (f *playerFile) stamped(own, previous playerEntry) playerEntry {
	e := playerEntry{}
	for k, v := range own {
		e[k] = v
	}
	if !f.isBanList {
		return e
	}
	e["source"] = "shulker"
	e["created"] = f.now().UTC().Format(banTimeLayout)
	if created, ok := previous["created"]; ok {
		e["created"] = created
	}
	if src, ok := previous["source"]; ok {
		e["source"] = src
	}
	return e
}

func driftFields(e playerEntry) playerEntry {
	out := playerEntry{}
	for k, v := range e {
		switch k {
		case "created", "source":
			continue
		case "expires":
			if s, ok := v.(string); ok {
				if t, err := time.Parse(banTimeLayout, s); err == nil {
					v = t.UTC().Format(banTimeLayout)
				}
			}
		}
		out[k] = v
	}
	return out
}

func parsePlayerEntries(data []byte) ([]playerEntry, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var list []playerEntry
	if err := dec.Decode(&list); err != nil {
		return nil, fmt.Errorf("player file: %w", err)
	}
	return list, nil
}

func entryUUID(e playerEntry) string {
	u, _ := e["uuid"].(string)
	return player.Dashed(u)
}

func (b *Builder) collectPlayers(players *manifest.Players, desired map[string]source) error {
	if players == nil {
		return nil
	}
	resolve := func(p manifest.Player) (playerEntry, string, error) {
		locked, ok := player.Find(b.Lock.Players, player.Ref{Name: p.Name, UUID: p.UUID})
		if !ok {
			e := out.Errorf("player-unresolved", "player %s is not in the lock", player.Ref{Name: p.Name, UUID: p.UUID})
			e.Help = "run `shulker player` or `shulker build`"
			return nil, "", e
		}
		return playerEntry{"uuid": locked.UUID, "name": locked.Name}, locked.UUID, nil
	}
	whitelist := newPlayerFile(false)
	for _, p := range players.Whitelist {
		e, u, err := resolve(p)
		if err != nil {
			return err
		}
		whitelist.add(u, e)
	}
	ops := newPlayerFile(false)
	for _, o := range players.Ops {
		e, u, err := resolve(o.Player)
		if err != nil {
			return err
		}
		level := o.Level
		if level == 0 {
			level = 4
		}
		e["level"] = level
		e["bypassesPlayerLimit"] = o.BypassesPlayerLimit
		ops.add(u, e)
	}
	bans := newPlayerFile(true)
	for _, ban := range players.Bans {
		e, u, err := resolve(ban.Player)
		if err != nil {
			return err
		}
		e["expires"] = "forever"
		if ban.Expires != "" {
			t, err := time.Parse(time.RFC3339, ban.Expires)
			if err != nil {
				return out.Errorf("players-invalid", "ban for %s: expires %q is not an RFC 3339 timestamp", e["name"], ban.Expires)
			}
			e["expires"] = t.UTC().Format(banTimeLayout)
		}
		e["reason"] = "Banned by an operator."
		if ban.Reason != "" {
			e["reason"] = ban.Reason
		}
		bans.add(u, e)
	}
	desired[WhitelistFile] = ownedSource(whitelist)
	desired[OpsFile] = ownedSource(ops)
	desired[BansFile] = ownedSource(bans)
	return nil
}
