package cache

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"shulker.sh/shulker/internal/fsutil"
)

// A Listing is one item in a provider's catalogue, known by the provider's id for it.
type Listing struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
}

// Proofs of a ListingPair: the two listings' jars carry the same mod id, or their files hash the same.
const (
	ProofJarID = "jar-id"
	ProofHash  = "hash"
)

// A ListingPair is two providers' listings of one item, as a jar id or a file hash proved them.
type ListingPair struct {
	Listings [2]Listing `json:"listings"`
	Type     string     `json:"type"`
	Proof    string     `json:"proof"`
	ModID    string     `json:"modId,omitempty"`
	LastUsed string     `json:"lastUsed"`
}

func (p ListingPair) pairs(a, b Listing) bool {
	return p.Listings == [2]Listing{a, b} || p.Listings == [2]Listing{b, a}
}

// ListingIndex is the listing index: every pair shulker has proved, looked up by either listing.
type ListingIndex struct {
	Pairs []ListingPair `json:"pairs"`
}

// listingsUnused is how long a pair goes unused before a prune drops it.
const listingsUnused = 90 * 24 * time.Hour

func today() string {
	return time.Now().UTC().Format(time.DateOnly)
}

// Paired reports whether the index pairs a and b.
func (ix *ListingIndex) Paired(a, b Listing) bool {
	_, ok := ix.pair(a, b)
	return ok
}

func (ix *ListingIndex) pair(a, b Listing) (int, bool) {
	if ix == nil {
		return 0, false
	}
	i := slices.IndexFunc(ix.Pairs, func(p ListingPair) bool { return p.pairs(a, b) })
	return i, i >= 0
}

// Partners are the listings the index pairs l with.
func (ix *ListingIndex) Partners(l Listing) []Listing {
	if ix == nil {
		return nil
	}
	var partners []Listing
	for _, p := range ix.Pairs {
		switch l {
		case p.Listings[0]:
			partners = append(partners, p.Listings[1])
		case p.Listings[1]:
			partners = append(partners, p.Listings[0])
		}
	}
	return partners
}

// Stale reports whether a UseListings of a and b would change anything, which it wouldn't for a
// pair already used today.
func (ix *ListingIndex) Stale(a, b Listing) bool {
	i, ok := ix.pair(a, b)
	return ok && ix.Pairs[i].LastUsed != today()
}

// ReadListings is the listing index. A missing or unreadable file is an empty index: every pair in
// it is proved again the next time it is needed.
func (c *Cache) ReadListings() (*ListingIndex, error) {
	data, err := os.ReadFile(c.ListingIndex())
	if errors.Is(err, fs.ErrNotExist) {
		return &ListingIndex{}, nil
	}
	if err != nil {
		return nil, err
	}
	var ix ListingIndex
	if json.Unmarshal(data, &ix) != nil {
		return &ListingIndex{}, nil
	}
	return &ix, nil
}

// RecordListings adds each pair to the index, or refreshes the one it holds, as used today.
func (c *Cache) RecordListings(pairs ...ListingPair) error {
	return c.updateListings(func(ix *ListingIndex) bool {
		for _, p := range pairs {
			p.LastUsed = today()
			if i, ok := ix.pair(p.Listings[0], p.Listings[1]); ok {
				ix.Pairs[i] = p
				continue
			}
			ix.Pairs = append(ix.Pairs, p)
		}
		return len(pairs) > 0
	})
}

// UseListings marks the pairs the index holds for these listings as used today.
func (c *Cache) UseListings(pairs ...[2]Listing) error {
	return c.updateListings(func(ix *ListingIndex) bool {
		changed := false
		for _, p := range pairs {
			if i, ok := ix.pair(p[0], p[1]); ok && ix.Pairs[i].LastUsed != today() {
				ix.Pairs[i].LastUsed = today()
				changed = true
			}
		}
		return changed
	})
}

// pruneListings drops the pairs unused for listingsUnused, returning how many went.
func (c *Cache) pruneListings(dryRun bool) (int, error) {
	cutoff := time.Now().UTC().Add(-listingsUnused).Format(time.DateOnly)
	unused := func(p ListingPair) bool { return p.LastUsed < cutoff }
	if dryRun {
		ix, err := c.ReadListings()
		if err != nil {
			return 0, err
		}
		return len(slices.DeleteFunc(slices.Clone(ix.Pairs), func(p ListingPair) bool { return !unused(p) })), nil
	}
	dropped := 0
	err := c.updateListings(func(ix *ListingIndex) bool {
		before := len(ix.Pairs)
		ix.Pairs = slices.DeleteFunc(ix.Pairs, unused)
		dropped = before - len(ix.Pairs)
		return dropped > 0
	})
	return dropped, err
}

// updateListings rereads the index under its lock, so two commands recording at once both land,
// and writes it back when change reports it changed.
func (c *Cache) updateListings(change func(*ListingIndex) bool) error {
	path := c.ListingIndex()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	unlock, err := fsutil.Lock(path + ".lock")
	if err != nil {
		return err
	}
	defer unlock()
	ix, err := c.ReadListings()
	if err != nil {
		return err
	}
	if !change(ix) {
		return nil
	}
	return fsutil.WriteJSON(path, ix)
}
