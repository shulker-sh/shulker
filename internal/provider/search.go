package provider

import (
	"context"

	"shulker.sh/shulker/internal/out"
)

// Hit is one project a search found, with the provider it was found on.
type Hit struct {
	Provider string
	Project
}

// Search asks each named provider for query. A provider that can't be asked is
// skipped with its reason, one that fails is left out with its failure, and
// searched names those that answered, in the order asked. step, when set, runs
// before each provider is asked and the func it returns after, with that
// provider's failure or nil, so a caller can draw each search as a step line.
func (ps Providers) Search(ctx context.Context, names []string, query, kind string, limit int, step func(name string) func(error)) (hits []Hit, searched []string, skipped []*out.Error, failures []error) {
	for _, name := range names {
		p, err := ps.Get(name)
		if err != nil {
			skipped = append(skipped, out.AsError(err))
			continue
		}
		done := func(error) {}
		if step != nil {
			done = step(name)
		}
		found, err := p.Search(ctx, query, kind, limit)
		done(err)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		searched = append(searched, name)
		for _, proj := range found {
			hits = append(hits, Hit{Provider: name, Project: proj})
		}
	}
	return hits, searched, skipped, failures
}
