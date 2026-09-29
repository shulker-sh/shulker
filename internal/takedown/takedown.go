// Package takedown asks each provider whether it still has the files a lock holds from it. A
// provider deletes a file it finds to be malware, but authors delete their own old versions too,
// and the API doesn't say which, so a file gone is a reason to look, not proof.
package takedown

import (
	"context"
	"maps"
	"slices"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

// Entry is a file a lock holds from a provider.
type Entry struct {
	Key      string `json:"key"`
	Type     string `json:"type"`
	Provider string `json:"provider"`
	Project  string `json:"project"`
	Version  string `json:"version"`
	// Number is the version as its provider names it, for a report.
	Number string `json:"versionNumber"`
	Sha512 string `json:"sha512"`
}

// Entries are the files l holds from a provider: its hosted modpacks, its mods, then each kind of
// pack, each by key. An entry still waiting for a manual download has no file to ask about.
func Entries(l *lock.Lock) []Entry {
	var entries []Entry
	add := func(key, kind, providerName, project, version, number, sha512 string) {
		if providerName != "" && project != "" && sha512 != "" {
			entries = append(entries, Entry{Key: key, Type: kind, Provider: providerName, Project: project, Version: version, Number: number, Sha512: sha512})
		}
	}
	for _, key := range slices.Sorted(maps.Keys(l.Modpacks)) {
		mp := l.Modpacks[key]
		add(key, manifest.TypeModpack, mp.Provider, mp.Project, mp.Version, mp.VersionNumber, mp.Sha512)
	}
	for _, key := range slices.Sorted(maps.Keys(l.Mods)) {
		m := l.Mods[key]
		add(key, manifest.TypeMod, m.Provider, m.Project, m.Version, m.VersionNumber, m.Sha512)
	}
	for _, kind := range manifest.PackKinds {
		section := l.Packs(kind)
		for _, key := range slices.Sorted(maps.Keys(section)) {
			p := section[key]
			add(key, kind, p.Provider, p.Project, p.Version, p.VersionNumber, p.Sha512)
		}
	}
	return entries
}

// Status is what a provider said about a file.
type Status string

const (
	// Present is a file the provider files under the project the lock names.
	Present Status = "present"
	// Gone is a file the provider no longer knows.
	Gone Status = "gone"
	// Moved is a file the provider files under another project than the lock names.
	Moved Status = "moved"
	// Unchecked is a file the provider couldn't be asked about.
	Unchecked Status = "unchecked"
)

// File is one entry with what its provider said about it.
type File struct {
	Entry
	Status Status `json:"status"`
	// FiledUnder is the project the provider files a moved file under.
	FiledUnder string `json:"filedUnder,omitempty"`
}

// Skipped is a provider that wasn't asked, and why: shulker is offline, the provider can't be
// reached, or it isn't set up.
type Skipped struct {
	Provider string `json:"provider"`
	Reason   string `json:"reason"`
}

// Result is what every provider said about the entries asked about.
type Result struct {
	Files   []File    `json:"files"`
	Skipped []Skipped `json:"skipped"`
}

// With are the files with status s.
func (r Result) With(s Status) []File {
	var files []File
	for _, f := range r.Files {
		if f.Status == s {
			files = append(files, f)
		}
	}
	return files
}

// Check asks each provider, in one request, where it files every entry from it, reading a file's
// bytes from c for a provider that indexes content. Entries sharing a file are asked about once.
// A provider that can't be asked leaves its entries unchecked rather than failing the check.
func Check(ctx context.Context, ps provider.Providers, c *cache.Cache, entries []Entry) Result {
	res := Result{Files: make([]File, len(entries)), Skipped: []Skipped{}}
	byProvider := map[string][]int{}
	for i, e := range entries {
		res.Files[i] = File{Entry: e, Status: Unchecked}
		byProvider[e.Provider] = append(byProvider[e.Provider], i)
	}
	for _, name := range slices.Sorted(maps.Keys(byProvider)) {
		p, ok := ps[name]
		if !ok {
			res.Skipped = append(res.Skipped, Skipped{Provider: name, Reason: "shulker doesn't know this provider"})
			continue
		}
		if err := p.Available(); err != nil {
			res.Skipped = append(res.Skipped, Skipped{Provider: name, Reason: out.AsError(err).Message})
			continue
		}
		files := map[string]provider.LockedFile{}
		for _, i := range byProvider[name] {
			e := entries[i]
			f := provider.LockedFile{Type: e.Type, Sha512: e.Sha512}
			if c != nil && c.Has(e.Sha512) {
				f.Path = c.Object(e.Sha512)
			}
			files[e.Sha512] = f
		}
		found, unchecked, err := p.Filed(ctx, files)
		if err != nil {
			res.Skipped = append(res.Skipped, Skipped{Provider: name, Reason: out.AsError(err).Message})
			continue
		}
		for _, i := range byProvider[name] {
			f := &res.Files[i]
			filing, ok := found[f.Sha512]
			switch {
			case slices.Contains(unchecked, f.Sha512):
			case !ok:
				f.Status = Gone
			case filing.Project != f.Project:
				f.Status, f.FiledUnder = Moved, filing.Project
			default:
				f.Status = Present
			}
		}
	}
	return res
}
