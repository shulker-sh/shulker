package build

import (
	"cmp"
	"maps"
	"path/filepath"
	"slices"
	"time"

	"shulker.sh/shulker/internal/build/marker"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/takedown"
)

// changelogSpan is how long a sync's changes stay in the directory's changelog.
const changelogSpan = 30 * 24 * time.Hour

// LockedEntries are every entry of l: its modpacks, its mods, then each kind of pack, each by key.
func LockedEntries(l *lock.Lock) []instance.LockedEntry {
	entries := []instance.LockedEntry{}
	for _, key := range slices.Sorted(maps.Keys(l.Modpacks)) {
		mp := l.Modpacks[key]
		entries = append(entries, instance.LockedEntry{Key: key, Type: manifest.TypeModpack, Provider: mp.Provider, Project: mp.Project})
	}
	for _, key := range slices.Sorted(maps.Keys(l.Mods)) {
		m := l.Mods[key]
		entries = append(entries, instance.LockedEntry{Key: key, Type: manifest.TypeMod, Provider: m.Provider, Project: m.Project})
	}
	for _, kind := range manifest.PackKinds {
		section := l.Packs(kind)
		for _, key := range slices.Sorted(maps.Keys(section)) {
			p := section[key]
			entries = append(entries, instance.LockedEntry{Key: key, Type: kind, Provider: p.Provider, Project: p.Project})
		}
	}
	return entries
}

// Review is what building side would bring against what the directory's last build recorded: the
// entries it adds from a provider, the files no provider published that it adds, whether lock
// entries or jars and packs an override folder lays, and the entries now locked from another
// project. It is nil for a directory with no record to compare against, as on its first sync.
func (b *Builder) Review(side string, opts Options) (*instance.Changes, error) {
	prev := instance.LoadState(b.Target(side, opts.Dir))
	if prev.Entries == nil {
		return nil, nil
	}
	desired, _, err := b.collect(side, opts, &Report{Warnings: []string{}})
	if err != nil {
		return nil, err
	}
	was := map[string]instance.LockedEntry{}
	for _, e := range prev.Entries {
		was[e.Type+"/"+e.Key] = e
	}
	c := &instance.Changes{Added: []instance.Changed{}, Unpublished: []instance.Changed{}, Moved: []instance.Changed{}}
	for _, e := range LockedEntries(b.Lock) {
		before, had := was[e.Type+"/"+e.Key]
		changed := instance.Changed{Key: e.Key, Type: e.Type, Provider: e.Provider, Project: e.Project}
		switch {
		case e.Provider == "":
			if !had || before.Provider != "" {
				c.Unpublished = append(c.Unpublished, changed)
			}
		case !had:
			c.Added = append(c.Added, changed)
		case before.Provider != "" && (before.Provider != e.Provider || before.Project != e.Project):
			changed.WasProvider, changed.WasProject = before.Provider, before.Project
			c.Moved = append(c.Moved, changed)
		}
	}
	for _, rel := range slices.Sorted(maps.Keys(desired)) {
		if desired[rel].origin != "" && isJarOrPack(rel) && prev.Files[rel] == "" {
			c.Unpublished = append(c.Unpublished, instance.Changed{Path: rel})
		}
	}
	return c, nil
}

// changelog is log with latest added when it brought anything, less the syncs older than
// changelogSpan before now.
func changelog(log []instance.Changes, latest *instance.Changes, now time.Time) []instance.Changes {
	if !latest.IsEmpty() {
		log = append(slices.Clone(log), *latest)
	}
	return slices.DeleteFunc(slices.Clone(log), func(c instance.Changes) bool {
		at, err := time.Parse(time.RFC3339, c.At)
		return err != nil || now.Sub(at) > changelogSpan
	})
}

// changedSince are the jars directly in dir's mods/ whose bytes differ from what the last build
// recorded placing, the jars the build will keep and warn about.
func changedSince(dir string, prev instance.State) ([]string, error) {
	var changed []string
	for _, rel := range slices.Sorted(maps.Keys(prev.Files)) {
		if !isPlacedJar(rel) {
			continue
		}
		current, exists, err := fileSha256(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		if exists && current != prev.Files[rel] {
			changed = append(changed, rel)
		}
	}
	return changed, nil
}

// notices are what the marker tells the player about a directory: what its syncs of the last month
// brought, the files gone from their provider, and the jars changed since they were placed.
type notices struct {
	changelog   []instance.Changes
	takedowns   []takedown.File
	changedJars []string
}

// noticeSections are n as the marker's sections: one of standing notices, then one per day of changes,
// newest first. Each is left out when it has nothing to show.
func (b *Builder) noticeSections(n *notices) []marker.Section {
	if n == nil {
		return nil
	}
	var standing []marker.Item
	for _, f := range n.takedowns {
		text := f.Key + " is gone from " + b.Providers.Title(f.Provider)
		if f.Status == takedown.Moved {
			text = f.Key + " is filed under another project on " + b.Providers.Title(f.Provider)
		}
		standing = append(standing, marker.Item{Text: text})
	}
	for _, rel := range n.changedJars {
		standing = append(standing, marker.Item{Text: rel + " changed since shulker placed it"})
	}
	sections := []marker.Section{{Title: "Notices", Items: standing}}
	var days []string
	byDay := map[string][]marker.Item{}
	for _, c := range slices.Backward(n.changelog) {
		day := c.At
		if at, err := time.Parse(time.RFC3339, c.At); err == nil {
			day = at.Format(time.DateOnly)
		}
		if _, seen := byDay[day]; !seen {
			days = append(days, day)
		}
		for _, a := range c.Added {
			byDay[day] = append(byDay[day], marker.Item{Text: "Added " + a.Key, Note: b.Providers.Title(a.Provider)})
		}
		for _, u := range c.Unpublished {
			byDay[day] = append(byDay[day], marker.Item{Text: "Added " + cmp.Or(u.Key, u.Path), Note: "no provider"})
		}
		for _, m := range c.Moved {
			byDay[day] = append(byDay[day], marker.Item{Text: m.Key + " moved to project " + m.Project, Note: "was " + m.WasProject})
		}
	}
	for _, day := range days {
		sections = append(sections, marker.Section{Title: "Synced " + day, Items: byDay[day]})
	}
	return sections
}
