package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/andrewmast/shulker/internal/manifest"
	"github.com/andrewmast/shulker/internal/out"
	"github.com/andrewmast/shulker/internal/player"
)

const (
	WhitelistFile = "whitelist.json"
	OpsFile       = "ops.json"
	BansFile      = "banned-players.json"
	banTimeLayout = "2006-01-02 15:04:05 -0700"
)

type playerEntry map[string]any

type playerFile struct {
	order   []string
	entries map[string]playerEntry
	bans    bool
	now     func() time.Time
}

func newPlayerFile(bans bool) *playerFile {
	return &playerFile{entries: map[string]playerEntry{}, bans: bans, now: time.Now}
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

func (f *playerFile) canonical() []byte {
	list := make([]playerEntry, 0, len(f.order))
	for _, u := range f.keys() {
		list = append(list, driftFields(f.entries[u]))
	}
	data, _ := json.Marshal(list)
	return data
}

func (f *playerFile) current(existing []byte, recordedKeys []string) []byte {
	owned := map[string]bool{}
	if recordedKeys != nil {
		for _, k := range recordedKeys {
			owned[k] = true
		}
	} else {
		for _, k := range f.order {
			owned[k] = true
		}
	}
	list, err := parsePlayerEntries(existing)
	if err != nil {
		return existing
	}
	found := []playerEntry{}
	for _, e := range list {
		if !owned[entryUUID(e)] {
			continue
		}
		found = append(found, driftFields(e))
	}
	sort.Slice(found, func(i, j int) bool { return entryUUID(found[i]) < entryUUID(found[j]) })
	data, _ := json.Marshal(found)
	return data
}

func (f *playerFile) merge(existing []byte, recordedKeys []string) ([]byte, error) {
	list, err := parsePlayerEntries(existing)
	if err != nil {
		return nil, err
	}
	dropped := map[string]bool{}
	for _, k := range recordedKeys {
		if _, still := f.entries[k]; !still {
			dropped[k] = true
		}
	}
	merged := []playerEntry{}
	seen := map[string]bool{}
	for _, e := range list {
		u := entryUUID(e)
		if dropped[u] {
			continue
		}
		own, ok := f.entries[u]
		if !ok {
			merged = append(merged, e)
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
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(merged); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (f *playerFile) stamped(own, previous playerEntry) playerEntry {
	e := playerEntry{}
	for k, v := range own {
		e[k] = v
	}
	if !f.bans {
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
			return nil, "", out.Errorf("player-unresolved", "player %s is not in the lock; run `shulker player` or `shulker build`", player.Ref{Name: p.Name, UUID: p.UUID})
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
				return out.Errorf("invalid-players", "ban for %s: expires %q is not an RFC 3339 timestamp", e["name"], ban.Expires)
			}
			e["expires"] = t.UTC().Format(banTimeLayout)
		}
		e["reason"] = "Banned by an operator."
		if ban.Reason != "" {
			e["reason"] = ban.Reason
		}
		bans.add(u, e)
	}
	desired[WhitelistFile] = source{owned: whitelist}
	desired[OpsFile] = source{owned: ops}
	desired[BansFile] = source{owned: bans}
	return nil
}
