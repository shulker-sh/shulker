package project

import (
	"maps"
	"slices"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
)

// ListEntry is one row of a listing: a modpack with its status, a mod or pack
// spanning the manifest and the lock, with lock values filling the gaps.
type ListEntry struct {
	Key        string              `json:"key"`
	Type       string              `json:"type"`
	Listed     bool                `json:"listed"`
	Version    string              `json:"version,omitempty"`
	File       string              `json:"file,omitempty"`
	Source     string              `json:"source,omitempty"`
	Kind       modpack.Kind        `json:"kind,omitempty"`
	Ref        string              `json:"ref,omitempty"`
	Path       string              `json:"path,omitempty"`
	State      string              `json:"state,omitempty"`
	Side       string              `json:"side,omitempty"`
	Channel    string              `json:"channel,omitempty"`
	Provider   string              `json:"provider,omitempty"`
	Pinned     bool                `json:"pinned,omitempty"`
	Modpack    string              `json:"modpack,omitempty"`
	RequiredBy []string            `json:"requiredBy,omitempty"`
	OS         manifest.StringList `json:"os,omitempty"`
	Feature    manifest.StringList `json:"feature,omitempty"`
}

// ModpackStatus is where one modpack stands against the lock; pinned and
// locked are the lock's entry, if the lock has one.
type ModpackStatus func(key string, req manifest.Require, pinned lock.Modpack, locked bool) (modpack.Status, error)

// ListEntries is every row a listing of kind shows, or of every kind when
// kind is empty: modpacks first, then mods, then the packs of each kind.
func ListEntries(p *Project, kind string, status ModpackStatus) ([]ListEntry, error) {
	res := []ListEntry{}
	modpacks := p.Manifest.Modpacks()
	if kind == "" || kind == manifest.TypeModpack {
		for _, key := range slices.Sorted(maps.Keys(modpacks)) {
			var pinned lock.Modpack
			locked := false
			if p.Lock != nil {
				pinned, locked = p.Lock.Modpacks[key]
			}
			st, err := status(key, modpacks[key], pinned, locked)
			if err != nil {
				return nil, err
			}
			e := ListEntry{
				Key: key, Type: manifest.TypeModpack, Listed: true, Version: st.Pin,
				Source: st.Source, Kind: st.Kind, Ref: st.Ref, Path: st.Path, State: st.State,
			}
			if st.Kind == modpack.Hosted {
				e.Source, e.Provider = "", st.Source
				e.Channel, e.Pinned = modpacks[key].Channel, modpacks[key].Pin != ""
			}
			res = append(res, e)
		}
	}
	if kind == "" || kind == manifest.TypeMod {
		mods := p.Manifest.Mods()
		keys := map[string]bool{}
		for key := range mods {
			keys[key] = true
		}
		if p.Lock != nil {
			for key := range p.Lock.Mods {
				keys[key] = true
			}
		}
		for _, key := range slices.Sorted(maps.Keys(keys)) {
			entry, listed := mods[key]
			e := ListEntry{
				Key: key, Type: manifest.TypeMod, Listed: listed, File: entry.File, Side: entry.Side,
				Channel: entry.Channel, Provider: entry.Provider, Pinned: entry.Pin != "",
				OS: entry.OS, Feature: entry.Feature,
			}
			if p.Lock != nil {
				if m, ok := p.Lock.Mods[key]; ok {
					e.Version = m.VersionNumber
					if e.File == "" {
						e.File = m.File
					}
					if e.Side == "" {
						e.Side = m.Side
					}
					if e.Provider == "" {
						e.Provider = m.Provider
					}
					if !listed {
						e.Modpack, e.RequiredBy = origin(m.RequiredBy, modpacks)
						if m.Modpack != "" {
							e.Modpack = m.Modpack
						}
					}
				}
			}
			res = append(res, e)
		}
	}
	for _, packKind := range manifest.PackKinds {
		if kind == "" || kind == packKind {
			res = append(res, packEntries(p, packKind)...)
		}
	}
	return res, nil
}

// packEntries lists the packs of one kind, spanning what shulker.json
// lists and what the lock holds, so one a locked modpack supplied shows up too.
func packEntries(p *Project, kind string) []ListEntry {
	listed := p.Manifest.Packs(kind)
	var locked map[string]lock.Pack
	if p.Lock != nil {
		locked = p.Lock.Packs(kind)
	}
	keys := map[string]bool{}
	for key := range listed {
		keys[key] = true
	}
	for key := range locked {
		keys[key] = true
	}
	res := []ListEntry{}
	for _, key := range slices.Sorted(maps.Keys(keys)) {
		entry, isListed := listed[key]
		e := ListEntry{
			Key: key, Type: kind, Listed: isListed, File: entry.File, Channel: entry.Channel,
			Provider: entry.Provider, Pinned: entry.Pin != "", OS: entry.OS, Feature: entry.Feature,
		}
		if lp, ok := locked[key]; ok {
			e.Version = lp.VersionNumber
			if e.File == "" {
				e.File = lp.File
			}
			if e.Provider == "" {
				e.Provider = lp.Provider
			}
			e.Modpack = lp.Modpack
		}
		res = append(res, e)
	}
	return res
}

// origin splits the mods and modpacks that pulled an entry in, so a row says
// where it comes from rather than listing both kinds together.
func origin(requiredBy []string, modpacks map[string]manifest.Require) (string, []string) {
	var from string
	var mods []string
	for _, by := range requiredBy {
		if _, isModpack := modpacks[by]; isModpack && from == "" {
			from = by
			continue
		}
		mods = append(mods, by)
	}
	return from, mods
}
