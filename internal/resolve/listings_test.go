package resolve

import (
	"slices"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/env/envtest"
	"shulker.sh/shulker/internal/provider"
)

var (
	alphaFabricAPI = cache.Listing{Provider: "alpha", ID: "a-fapi"}
	betaFabricAPI  = cache.Listing{Provider: "beta", ID: "306612"}
)

func (h *harness) listings() *cache.ListingIndex {
	h.t.Helper()
	ix, err := h.r.Cache.ReadListings()
	if err != nil {
		h.t.Fatal(err)
	}
	return ix
}

func (h *harness) fetches(line string) int {
	return len(slices.DeleteFunc(slices.Clone(h.log), func(l string) bool { return l != line }))
}

func TestAJarIDMatchGoesInTheListingIndex(t *testing.T) {
	_, _, h := twoHosts(t)
	h.mustAdd("sodium", AddOptions{})
	if len(h.listings().Pairs) != 0 {
		t.Fatalf("one provider's add proves no pair: %+v", h.listings().Pairs)
	}
	h.mustAdd("jei", AddOptions{})

	ix := h.listings()
	if len(ix.Pairs) != 1 || !ix.Paired(alphaFabricAPI, betaFabricAPI) {
		t.Fatalf("pairs = %+v", ix.Pairs)
	}
	if p := ix.Pairs[0]; p.Type != "mod" || p.Proof != cache.ProofJarID || p.ModID != "fabric-api" {
		t.Fatalf("pair = %+v", p)
	}
}

func TestASwitchOfProviderGoesInTheListingIndex(t *testing.T) {
	_, _, h := twoHosts(t)
	h.mustAdd("sodium", AddOptions{})
	h.mustAdd("sodium", AddOptions{Provider: "beta"})
	if !h.listings().Paired(cache.Listing{Provider: "alpha", ID: "a-sodium"}, cache.Listing{Provider: "beta", ID: "394468"}) {
		t.Fatalf("pairs = %+v", h.listings().Pairs)
	}
}

func TestAnIndexedPairIsAliasedWithoutFetchingItsJar(t *testing.T) {
	_, _, h := twoHosts(t)
	if err := h.r.Cache.RecordListings(cache.ListingPair{Listings: [2]cache.Listing{alphaFabricAPI, betaFabricAPI}, Type: "mod", Proof: cache.ProofJarID, ModID: "fabric-api"}); err != nil {
		t.Fatal(err)
	}
	h.mustAdd("sodium", AddOptions{})
	h.mustAdd("jei", AddOptions{})

	if fapi := h.mod("fabric-api"); fapi.Provider != "alpha" || fapi.Aliases["beta"] != "306612" || !slices.Contains(fapi.RequiredBy, "jei") {
		t.Fatalf("fabric-api lock entry: %+v", fapi)
	}
	if n := h.fetches("fetching fabric-api 0.130.0"); n != 1 {
		t.Fatalf("fabric-api was fetched %d times, want once: %v", n, h.log)
	}
	if !h.logged("keeping fabric-api 0.130.0 from alpha (beta project 306612 recorded as an alias)") {
		t.Fatalf("log: %v", h.log)
	}
}

func TestAHashMatchGoesInTheListingIndex(t *testing.T) {
	c := envtest.NewCDN(t)
	alpha := envtest.NewHost(c, "alpha")
	cf := envtest.NewHost(c, "curse").LikeCurseForge()
	iris := modJar(t, "iris", "1.0.0", "client")
	alpha.Publish(mod("YL57", "irisshaders"), provider.Version{ID: "v1", Number: "1.0.0", File: provider.File{Filename: "iris-1.0.0.jar"}}, iris)
	cf.PublishManual(mod("300002", "iris-cf"), provider.Version{ID: "5100002", Number: "1.0.0", File: provider.File{Filename: "iris-1.0.0.jar"}}, iris)
	h := newHarness(t, alpha, cf)

	h.mustAdd("iris-cf", AddOptions{Provider: "curse"})
	ix := h.listings()
	if len(ix.Pairs) != 1 || !ix.Paired(cache.Listing{Provider: "curse", ID: "300002"}, cache.Listing{Provider: "alpha", ID: "YL57"}) || ix.Pairs[0].Proof != cache.ProofHash {
		t.Fatalf("pairs = %+v", ix.Pairs)
	}
}
