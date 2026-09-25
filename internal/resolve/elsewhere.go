package resolve

import (
	"context"
	"fmt"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

// hostedElsewhere finds, on the manifest's other providers in its order, the version hosting the
// same bytes as v, for a file p's author blocks from third-party download: its sha1 is looked up
// on each, and the first hit wins. A provider that can't be asked is skipped.
func (r *Resolver) hostedElsewhere(ctx context.Context, p provider.Provider, v *provider.Version) *hosted {
	if v.File.Sha1 == "" {
		return nil
	}
	for _, name := range r.Manifest.ProviderOrder() {
		if name == p.Name() {
			continue
		}
		q, err := r.Providers.Get(name)
		if err != nil {
			continue
		}
		r.log("looking %s up on %s by hash", v.File.Filename, q.Title())
		found, err := q.IdentifySHA1(ctx, map[string]string{v.File.Filename: v.File.Sha1})
		if err != nil {
			r.Warnings = append(r.Warnings, fmt.Sprintf("%s wasn't looked up on %s (%s)", v.File.Filename, q.Title(), out.AsError(err).Message))
			continue
		}
		if h, ok := found[v.File.Filename]; ok && h.Version.File.URL != "" {
			return &hosted{p: q, proj: &h.Project, v: &h.Version}
		}
	}
	return nil
}

// placeAnywhere is place, and for a file its author blocks from third-party download, place from
// the provider hostedElsewhere finds, warning that it did. It returns the hosting it placed from.
func (r *Resolver) placeAnywhere(ctx context.Context, h hosted, key, requiredBy, side, channel string, replace bool, warn func(string)) (hosted, string, *lock.Mod, error) {
	id, prior, err := r.place(ctx, h.p, h.proj, h.v, key, requiredBy, side, channel, replace)
	if out.CodeOf(err) != "manual-download" {
		return h, id, prior, err
	}
	elsewhere := r.hostedElsewhere(ctx, h.p, h.v)
	if elsewhere == nil {
		return h, id, prior, err
	}
	warn(fmt.Sprintf("%s: %s doesn't allow third-party downloads of %s; locked from %s as %s instead", h.proj.Slug, h.p.Title(), h.v.File.Filename, elsewhere.p.Title(), elsewhere.proj.Slug))
	id, prior, err = r.place(ctx, elsewhere.p, elsewhere.proj, elsewhere.v, key, requiredBy, side, channel, replace)
	return *elsewhere, id, prior, err
}
