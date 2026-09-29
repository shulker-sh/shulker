package build

import (
	"context"
	"slices"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/takedown"
)

// RootedFile is a locked file the takedown check flagged, with the keys it is locked under and
// the roots that lock it.
type RootedFile struct {
	takedown.File
	Keys  []string `json:"keys"`
	Roots []string `json:"roots"`
}

// CacheCheck is what `cache verify` found: objects whose bytes changed, files gone from their
// provider or filed under another project, and objects no root uses.
type CacheCheck struct {
	Objects int            `json:"objects"`
	Changed []cache.Object `json:"changed"`
	// Dropped says the changed objects were removed, so the next build downloads them again.
	Dropped   bool               `json:"dropped"`
	Takedowns []RootedFile       `json:"takedowns"`
	Moved     []RootedFile       `json:"moved"`
	Skipped   []takedown.Skipped `json:"skipped"`
	Unused    []cache.Object     `json:"unused"`
	// UnusedBytes is the size of Unused together, what `cache prune` frees of objects.
	UnusedBytes int64 `json:"unusedBytes"`
}

// Fails reports whether the check found something to act on: a changed object it didn't drop, or
// a file gone from its provider or filed under another project. Unused objects are prune's.
func (v *CacheCheck) Fails() bool {
	return len(v.Changed) > 0 && !v.Dropped || len(v.Takedowns)+len(v.Moved) > 0
}

// Verify rehashes every cached object, dropping the changed ones with drop, asks each provider
// once about the files every root locks, and lists the objects no root uses. A changed object is
// left out of the takedown check, since a provider that indexes content would find it gone.
func (r Roots) Verify(ctx context.Context, c *cache.Cache, ps provider.Providers, drop bool) (*CacheCheck, error) {
	changed, hashed, err := c.Rehash()
	if err != nil {
		return nil, err
	}
	v := &CacheCheck{Objects: hashed, Changed: append([]cache.Object{}, changed...), Takedowns: []RootedFile{}, Moved: []RootedFile{}, Unused: []cache.Object{}}
	isChanged := map[string]bool{}
	for _, o := range changed {
		isChanged[o.Sha512] = true
		if drop {
			if err := c.Drop(o.Sha512); err != nil {
				return nil, err
			}
		}
	}
	v.Dropped = drop && len(changed) > 0
	var entries []takedown.Entry
	var rootOf []string
	for _, root := range r.Locks {
		for _, e := range takedown.Entries(root.Lock) {
			if !isChanged[e.Sha512] {
				entries = append(entries, e)
				rootOf = append(rootOf, root.Name)
			}
		}
	}
	checked := takedown.Check(ctx, ps, c, entries)
	v.Skipped = checked.Skipped
	byFile := map[string]int{}
	for i, f := range checked.Files {
		var into *[]RootedFile
		switch f.Status {
		case takedown.Gone:
			into = &v.Takedowns
		case takedown.Moved:
			into = &v.Moved
		default:
			continue
		}
		id := string(f.Status) + "/" + f.Provider + "/" + f.Sha512
		at, seen := byFile[id]
		if !seen {
			at = len(*into)
			byFile[id] = at
			*into = append(*into, RootedFile{File: f})
		}
		rf := &(*into)[at]
		if !slices.Contains(rf.Keys, f.Key) {
			rf.Keys = append(rf.Keys, f.Key)
		}
		if !slices.Contains(rf.Roots, rootOf[i]) {
			rf.Roots = append(rf.Roots, rootOf[i])
		}
	}
	unused, err := c.Unused(r.Locks)
	if err != nil {
		return nil, err
	}
	for _, o := range unused {
		v.Unused = append(v.Unused, o)
		v.UnusedBytes += o.Size
	}
	return v, nil
}
