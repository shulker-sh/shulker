package cache

import (
	"slices"
	"testing"
	"time"

	"shulker.sh/shulker/internal/fsutil"
)

var (
	modrinthSodium   = Listing{Provider: "modrinth", ID: "AANobbMI"}
	curseforgeSodium = Listing{Provider: "curseforge", ID: "394468"}
	modrinthIris     = Listing{Provider: "modrinth", ID: "YL57xq9U"}
	curseforgeIris   = Listing{Provider: "curseforge", ID: "455508"}
)

func sodiumPair() ListingPair {
	return ListingPair{Listings: [2]Listing{modrinthSodium, curseforgeSodium}, Type: "mod", Proof: ProofJarID, ModID: "sodium"}
}

func writeListings(t *testing.T, c *Cache, pairs ...ListingPair) {
	t.Helper()
	data, err := fsutil.MarshalJSON(ListingIndex{Pairs: pairs})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, c.ListingIndex(), string(data))
}

func readListings(t *testing.T, c *Cache) *ListingIndex {
	t.Helper()
	ix, err := c.ReadListings()
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

func TestRecordedListingsPairEitherWay(t *testing.T) {
	c := newCache(t)
	if err := c.RecordListings(sodiumPair()); err != nil {
		t.Fatal(err)
	}
	ix := readListings(t, c)
	if !ix.Paired(modrinthSodium, curseforgeSodium) || !ix.Paired(curseforgeSodium, modrinthSodium) {
		t.Fatalf("the recorded pair isn't found both ways: %+v", ix.Pairs)
	}
	if ix.Paired(modrinthSodium, curseforgeIris) {
		t.Fatal("a listing pairs with one it was never recorded with")
	}
	if got := ix.Partners(curseforgeSodium); !slices.Equal(got, []Listing{modrinthSodium}) {
		t.Fatalf("Partners = %v", got)
	}
	if ix.Pairs[0].LastUsed != today() {
		t.Fatalf("lastUsed = %q, want today", ix.Pairs[0].LastUsed)
	}
}

func TestRecordingAPairAgainKeepsOneEntry(t *testing.T) {
	c := newCache(t)
	writeListings(t, c, ListingPair{Listings: [2]Listing{curseforgeSodium, modrinthSodium}, Type: "mod", Proof: ProofHash, LastUsed: "2026-01-01"})
	if err := c.RecordListings(sodiumPair()); err != nil {
		t.Fatal(err)
	}
	ix := readListings(t, c)
	if len(ix.Pairs) != 1 || ix.Pairs[0].Proof != ProofJarID || ix.Pairs[0].LastUsed != today() {
		t.Fatalf("pairs = %+v", ix.Pairs)
	}
}

func TestUsingListingsTouchesOnlyWhatTheIndexHolds(t *testing.T) {
	c := newCache(t)
	writeListings(t, c, ListingPair{Listings: [2]Listing{modrinthSodium, curseforgeSodium}, Type: "mod", Proof: ProofJarID, LastUsed: "2026-01-01"})
	if !readListings(t, c).Stale(curseforgeSodium, modrinthSodium) {
		t.Fatal("a pair last used in January isn't stale")
	}
	if err := c.UseListings([2]Listing{curseforgeSodium, modrinthSodium}, [2]Listing{modrinthIris, curseforgeIris}); err != nil {
		t.Fatal(err)
	}
	ix := readListings(t, c)
	if len(ix.Pairs) != 1 || ix.Pairs[0].LastUsed != today() {
		t.Fatalf("pairs = %+v", ix.Pairs)
	}
	if ix.Stale(modrinthSodium, curseforgeSodium) {
		t.Fatal("a pair used today is stale")
	}
}

func TestAnUnreadableIndexIsEmpty(t *testing.T) {
	c := newCache(t)
	writeFile(t, c.ListingIndex(), "{not json")
	if ix := readListings(t, c); len(ix.Pairs) != 0 {
		t.Fatalf("pairs = %+v", ix.Pairs)
	}
	if err := c.RecordListings(sodiumPair()); err != nil {
		t.Fatal(err)
	}
	if ix := readListings(t, c); len(ix.Pairs) != 1 {
		t.Fatalf("pairs = %+v", ix.Pairs)
	}
}

func TestPruneDropsListingsUnusedFor90Days(t *testing.T) {
	c := newCache(t)
	day := func(ago int) string { return time.Now().UTC().AddDate(0, 0, -ago).Format(time.DateOnly) }
	old := ListingPair{Listings: [2]Listing{modrinthIris, curseforgeIris}, Type: "shader", Proof: ProofHash, LastUsed: day(91)}
	recent := sodiumPair()
	recent.LastUsed = day(89)
	writeListings(t, c, old, recent)

	would, err := c.Prune(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if would.Listings != 1 || len(readListings(t, c).Pairs) != 2 {
		t.Fatalf("dry run: would drop %d, index has %+v", would.Listings, readListings(t, c).Pairs)
	}
	pruned, err := c.Prune(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	ix := readListings(t, c)
	if pruned.Listings != 1 || pruned.Empty() || len(ix.Pairs) != 1 || !ix.Paired(modrinthSodium, curseforgeSodium) {
		t.Fatalf("pruned %d, index has %+v", pruned.Listings, ix.Pairs)
	}
	u, err := c.Usage()
	if err != nil {
		t.Fatal(err)
	}
	if u.Listings != 1 {
		t.Fatalf("Usage.Listings = %d", u.Listings)
	}
}
