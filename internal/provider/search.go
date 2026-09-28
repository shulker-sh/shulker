package provider

import (
	"cmp"
	"context"
	"regexp"
	"slices"
	"strings"
	"unicode"

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

// Result is one row of a search: a listing found on one provider, or the same listing's hits on
// several merged into one, in the order the providers were asked.
type Result struct {
	Hits []Hit
}

func (r Result) Slug() string  { return r.Hits[0].Slug }
func (r Result) Title() string { return r.Hits[0].Title }
func (r Result) Type() string  { return r.Hits[0].Type }

func (r Result) Side() string {
	for _, h := range r.Hits {
		if h.Side != "" {
			return h.Side
		}
	}
	return ""
}

func (r Result) Author() string {
	for _, h := range r.Hits {
		if h.Author != "" {
			return h.Author
		}
	}
	return ""
}

func (r Result) Summary() string {
	for _, h := range r.Hits {
		if h.Summary != "" {
			return h.Summary
		}
	}
	return ""
}

func (r Result) Downloads() int64 {
	var n int64
	for _, h := range r.Hits {
		n += h.Downloads
	}
	return n
}

// On is r's hit on the named provider.
func (r Result) On(name string) (Hit, bool) {
	for _, h := range r.Hits {
		if h.Provider == name {
			return h, true
		}
	}
	return Hit{}, false
}

// Merge folds hits from different providers into one Result when they are the same listing: the
// slug and type match and so does the normalized name or the author, or paired says so. Results
// are sorted by their summed downloads, most first.
func Merge(hits []Hit, paired func(a, b Hit) bool) []Result {
	var results []Result
	for _, hit := range hits {
		i := slices.IndexFunc(results, func(r Result) bool {
			if _, taken := r.On(hit.Provider); taken {
				return false
			}
			return slices.ContainsFunc(r.Hits, func(h Hit) bool {
				return isSameListing(h, hit) || (paired != nil && paired(h, hit))
			})
		})
		if i < 0 {
			results = append(results, Result{Hits: []Hit{hit}})
			continue
		}
		results[i].Hits = append(results[i].Hits, hit)
	}
	slices.SortStableFunc(results, func(a, b Result) int { return cmp.Compare(b.Downloads(), a.Downloads()) })
	return results
}

func isSameListing(a, b Hit) bool {
	if a.Slug != b.Slug || a.Type != b.Type {
		return false
	}
	return normalName(a.Title) == normalName(b.Title) || (a.Author != "" && strings.EqualFold(a.Author, b.Author))
}

// nameTag is a decoration a listing's name carries on one provider and not another: a trailing
// "(Fabric)" or "[1.21]", or an upper-case tag like " - DISCONTINUED".
var nameTag = regexp.MustCompile(`(\s*[(\[][^()\[\]]*[)\]]|\s+[-–|]\s+[A-Z][A-Z0-9 ]*)\s*$`)

// normalName is a name with its tags dropped, lower-cased and down to its letters and digits, so
// "Fresh Animations" and "fresh-animations (Fabric)" read the same.
func normalName(name string) string {
	for {
		trimmed := nameTag.ReplaceAllString(name, "")
		if trimmed == name {
			break
		}
		name = trimmed
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, name)
}
