package cli

import (
	"maps"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/pack"
	"shulker.sh/shulker/internal/project"
)

type listEntry struct {
	Key        string              `json:"key"`
	Type       string              `json:"type"`
	Listed     bool                `json:"listed"`
	Version    string              `json:"version,omitempty"`
	Source     string              `json:"source,omitempty"`
	Kind       pack.Kind           `json:"kind,omitempty"`
	Ref        string              `json:"ref,omitempty"`
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

func (a *app) listCmd() *cobra.Command { return a.listCmdFor("") }

func (a *app) listCmdFor(kind string) *cobra.Command {
	var typ string
	cmd := &cobra.Command{
		Use:         "list",
		Annotations: reads(),
		Aliases:     []string{"ls"},
		Short:       listShort(kind),
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			chosen, err := chooseType(cmd, kind, typ, "")
			if err != nil {
				return err
			}
			p, err := a.openProject()
			if err != nil {
				return err
			}
			res, err := a.listEntries(p, chosen)
			if err != nil {
				return err
			}
			return a.printer.Emit(res, func(l *out.Lines) {
				if len(res) == 0 {
					l.Info(emptyListText(chosen))
					return
				}
				printList(l, res)
			})
		},
	}
	if kind == "" {
		cmd.Flags().StringVar(&typ, "type", "", typeFlagUsage)
	}
	return cmd
}

func listShort(kind string) string {
	switch kind {
	case manifest.TypeModpack:
		return "List modpacks with their locked ref and whether a local modpack has changed"
	case "":
		return "List everything in requires with its locked version"
	}
	return "List the project's " + kind + "s with their locked versions"
}

func emptyListText(kind string) string {
	if kind == manifest.TypeModpack {
		return "No modpacks yet; add one with `shulker modpack add <source>`."
	}
	return "Nothing in requires yet; add a mod with `shulker add <mod>`."
}

func (a *app) listEntries(p *project.Project, kind string) ([]listEntry, error) {
	res := []listEntry{}
	modpacks := p.Manifest.Modpacks()
	if kind == "" || kind == manifest.TypeModpack {
		store, err := a.packStore(p)
		if err != nil {
			return nil, err
		}
		for _, key := range slices.Sorted(maps.Keys(modpacks)) {
			var pinned lock.Modpack
			locked := false
			if p.Lock != nil {
				pinned, locked = p.Lock.Modpacks[key]
			}
			st, err := store.Status(key, modpacks[key], pinned, locked)
			if err != nil {
				return nil, err
			}
			res = append(res, listEntry{
				Key: key, Type: manifest.TypeModpack, Listed: true, Version: st.Pin,
				Source: st.Source, Kind: st.Kind, Ref: st.Ref, State: st.State,
			})
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
			e := listEntry{
				Key: key, Type: manifest.TypeMod, Listed: listed, Side: entry.Side,
				Channel: entry.Channel, Provider: entry.Provider, Pinned: entry.Pin != nil,
				OS: entry.OS, Feature: entry.Feature,
			}
			if p.Lock != nil {
				if m, ok := p.Lock.Mods[key]; ok {
					e.Version = m.VersionNumber
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
	for _, packKind := range []string{manifest.TypeResourcePack, manifest.TypeShader} {
		if kind == "" || kind == packKind {
			res = append(res, packEntries(p, packKind)...)
		}
	}
	return res, nil
}

// packEntries lists the resource packs or shaders, spanning what shulker.json
// lists and what the lock holds, so one a locked modpack supplied shows up too.
func packEntries(p *project.Project, kind string) []listEntry {
	listed := p.Manifest.ResourcePacks()
	var locked map[string]lock.Pack
	if kind == manifest.TypeShader {
		listed = p.Manifest.Shaders()
	}
	if p.Lock != nil {
		locked = p.Lock.ResourcePacks
		if kind == manifest.TypeShader {
			locked = p.Lock.Shaders
		}
	}
	keys := map[string]bool{}
	for key := range listed {
		keys[key] = true
	}
	for key := range locked {
		keys[key] = true
	}
	res := []listEntry{}
	for _, key := range slices.Sorted(maps.Keys(keys)) {
		entry, isListed := listed[key]
		e := listEntry{
			Key: key, Type: kind, Listed: isListed, Channel: entry.Channel,
			Provider: entry.Provider, Pinned: entry.Pin != nil, OS: entry.OS, Feature: entry.Feature,
		}
		if lp, ok := locked[key]; ok {
			e.Version = lp.VersionNumber
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

func printList(l *out.Lines, res []listEntry) {
	blocks := []struct{ heading, kind string }{
		{"Modpacks", manifest.TypeModpack},
		{"Mods", manifest.TypeMod},
		{"Resource packs", manifest.TypeResourcePack},
		{"Shaders", manifest.TypeShader},
	}
	printed := false
	for _, b := range blocks {
		var items []out.Item
		for _, e := range res {
			if e.Type == b.kind {
				items = append(items, listItem(l, e))
			}
		}
		if len(items) == 0 {
			continue
		}
		if printed {
			l.Blank()
		}
		printed = true
		l.Heading(b.heading)
		l.Items(items...)
	}
}

func listItem(l *out.Lines, e listEntry) out.Item {
	if e.Type == manifest.TypeModpack {
		aside := []string{string(e.Kind), e.State}
		if e.Version != "" {
			aside = append(aside, "pinned "+e.Version)
		}
		if e.Ref != "" {
			aside = append(aside, "ref "+e.Ref)
		}
		return out.Item{Kind: out.Note, Name: e.Key, Text: l.T.Grey(e.Source), Aside: aside}
	}
	it := out.Item{Kind: out.Note, Name: e.Key, Version: e.Version}
	if e.Version == "" {
		it.Aside = append(it.Aside, "not locked")
	}
	if e.Side != "" && e.Side != "both" {
		it.Aside = append(it.Aside, e.Side+" only")
	}
	if text := conditionText("os", e.OS); text != "" {
		it.Aside = append(it.Aside, text)
	}
	if text := conditionText("feature", e.Feature); text != "" {
		it.Aside = append(it.Aside, text)
	}
	if e.Channel != "" && e.Channel != "release" {
		it.Aside = append(it.Aside, "channel: "+e.Channel)
	}
	if e.Pinned {
		it.Aside = append(it.Aside, "pinned")
	}
	switch {
	case e.Modpack != "":
		it.Aside = append(it.Aside, "from "+e.Modpack)
	case len(e.RequiredBy) > 0:
		it.Aside = append(it.Aside, "required by "+strings.Join(e.RequiredBy, ", "))
	}
	return it
}
