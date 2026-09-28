package cli

import (
	"strings"
	"testing"
	"time"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/fsutil"
)

var irisPair = [2]cache.Listing{{Provider: "modrinth", ID: "YL57xq9U"}, {Provider: "curseforge", ID: "300002"}}

func TestSearchMergesAPairTheListingIndexProved(t *testing.T) {
	h := newHarness(t)
	if n := len(searchJSON(t, h, "iris")["results"].([]any)); n != 2 {
		t.Fatalf("unpaired, iris is %d rows, want 2", n)
	}
	c := &cache.Cache{Dir: h.cache}
	if err := c.RecordListings(cache.ListingPair{Listings: irisPair, Type: "mod", Proof: cache.ProofHash}); err != nil {
		t.Fatal(err)
	}
	results := searchJSON(t, h, "iris")["results"].([]any)
	if len(results) != 1 || len(results[0].(map[string]any)["providers"].([]any)) != 2 {
		t.Fatalf("paired, iris is %v", results)
	}
}

func TestCacheInfoCountsTheListingIndexAndPruneDropsUnusedPairs(t *testing.T) {
	h := newInPlace(t)
	c := &cache.Cache{Dir: h.cache}
	if err := c.RecordListings(cache.ListingPair{Listings: irisPair, Type: "mod", Proof: cache.ProofHash}); err != nil {
		t.Fatal(err)
	}
	stdout := h.mustRun(t, "cache", "info")
	if !strings.Contains(stdout, "1 listing pair in the listing index") || !strings.Contains(stdout, "nothing to prune") {
		t.Fatalf("cache info: %s", stdout)
	}

	old := time.Now().UTC().AddDate(0, 0, -91).Format(time.DateOnly)
	data, err := fsutil.MarshalJSON(cache.ListingIndex{Pairs: []cache.ListingPair{{Listings: irisPair, Type: "mod", Proof: cache.ProofHash, LastUsed: old}}})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, c.ListingIndex(), string(data))
	if stdout := h.mustRun(t, "cache", "info"); !strings.Contains(stdout, "1 unused listing pair prunable") {
		t.Fatalf("cache info: %s", stdout)
	}
	if stdout := h.mustRun(t, "cache", "prune"); !strings.Contains(stdout, "Dropped 1 unused listing pair") {
		t.Fatalf("cache prune: %s", stdout)
	}
	if ix, err := c.ReadListings(); err != nil || len(ix.Pairs) != 0 {
		t.Fatalf("index after prune: %+v %v", ix, err)
	}
}
