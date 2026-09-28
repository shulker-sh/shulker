package resolve

import (
	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/provider"
)

// indexedMod is the lock's mod the listing index pairs providerName's project with: the same mod,
// proved once, so recording the project as its alias needn't fetch the jar again.
func (r *Resolver) indexedMod(providerName, projectID string) (string, bool, error) {
	if r.listings == nil {
		ix, err := r.Cache.ReadListings()
		if err != nil {
			return "", false, err
		}
		r.listings = ix
	}
	for _, partner := range r.listings.Partners(cache.Listing{Provider: providerName, ID: projectID}) {
		for _, id := range sortedKeys(r.Lock.Mods) {
			if m := r.Lock.Mods[id]; m.Provider == partner.Provider && m.Project == partner.ID {
				return id, true, nil
			}
		}
	}
	return "", false, nil
}

// keepIndexed keeps the locked mod id, recording p's project as its alias as the index proved it.
func (r *Resolver) keepIndexed(id, requiredBy string, p provider.Provider, proj *provider.Project) (*lock.Mod, error) {
	prior := r.Lock.Mods[id]
	if requiredBy != "" {
		r.Lock.AddRequiredBy(id, requiredBy)
	}
	r.keepAlias(id, p, proj)
	return &prior, r.Cache.UseListings([2]cache.Listing{{Provider: prior.Provider, ID: prior.Project}, {Provider: p.Name(), ID: proj.ID}})
}

// keepAlias keeps the locked mod id from its own provider, recording p's project as its alias.
func (r *Resolver) keepAlias(id string, p provider.Provider, proj *provider.Project) {
	aliased := r.Lock.Mods[id]
	setAlias(&aliased, p.Name(), proj.ID)
	r.Lock.Mods[id] = aliased
	r.log("keeping %s %s from %s (%s project %s recorded as an alias)", id, aliased.VersionNumber, aliased.Provider, p.Name(), proj.ID)
}

// recordJarID records in the listing index that m and p's project are one mod, as the jar id
// modID both carry proves.
func (r *Resolver) recordJarID(m lock.Mod, p provider.Provider, projectID, modID string) error {
	return r.recordListings(cache.ListingPair{
		Listings: [2]cache.Listing{{Provider: m.Provider, ID: m.Project}, {Provider: p.Name(), ID: projectID}},
		Type:     manifest.TypeMod,
		Proof:    cache.ProofJarID,
		ModID:    modID,
	})
}

// recordHash records in the listing index that two providers' projects are one listing, as a file
// both host with the same hash proves.
func (r *Resolver) recordHash(p provider.Provider, projectID string, q provider.Provider, proj provider.Project) error {
	return r.recordListings(cache.ListingPair{
		Listings: [2]cache.Listing{{Provider: p.Name(), ID: projectID}, {Provider: q.Name(), ID: proj.ID}},
		Type:     proj.Type,
		Proof:    cache.ProofHash,
	})
}

func (r *Resolver) recordListings(pair cache.ListingPair) error {
	r.listings = nil
	return r.Cache.RecordListings(pair)
}
