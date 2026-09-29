package resolve

import (
	"context"
	"fmt"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/out"
)

// hostedElsewhere finds, on the manifest's other providers in its order, the version hosting the
// same bytes as h's, for a file whose author blocks third-party download: its sha1 is looked up on
// each, and the first hit wins and goes in the listing index. A provider that can't be asked is
// skipped.
func (r *Resolver) hostedElsewhere(ctx context.Context, h hosted) (*hosted, error) {
	p, v := h.p, h.v
	if v.File.Sha1 == "" {
		return nil, nil
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
			r.Warnings = append(r.Warnings, fmt.Sprintf("%s wasn't looked up on %s (%s).", v.File.Filename, q.Title(), out.AsError(err).Message))
			continue
		}
		if there, ok := found[v.File.Filename]; ok && there.Version.File.URL != "" {
			return &hosted{p: q, proj: &there.Project, v: &there.Version}, r.recordHash(p, h.proj.ID, q, there.Project)
		}
	}
	return nil, nil
}

// placeAnywhere is place, and for a file its author blocks from third-party download, place from
// the provider hostedElsewhere finds, warning that it did. It returns the hosting it placed from.
func (r *Resolver) placeAnywhere(ctx context.Context, h hosted, key, requiredBy, side, channel string, replace bool, warn func(string)) (hosted, string, *lock.Mod, error) {
	id, prior, err := r.place(ctx, h.p, h.proj, h.v, key, requiredBy, side, channel, replace)
	if out.CodeOf(err) != "manual-download" {
		return h, id, prior, err
	}
	elsewhere, recordErr := r.hostedElsewhere(ctx, h)
	if recordErr != nil {
		return h, id, prior, recordErr
	}
	if elsewhere == nil {
		return h, id, prior, err
	}
	warn(fmt.Sprintf("%s: taken from %s (%s blocks third-party downloads)", h.proj.Slug, elsewhere.p.Title(), h.p.Title()))
	id, prior, err = r.place(ctx, elsewhere.p, elsewhere.proj, elsewhere.v, key, requiredBy, side, channel, replace)
	return *elsewhere, id, prior, err
}
