package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/resolve"
)

type searchHit struct {
	Provider  string `json:"provider"`
	ID        string `json:"id"`
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Type      string `json:"type,omitempty"`
	Side      string `json:"side,omitempty"`
	Downloads int64  `json:"downloads,omitempty"`
}

type searchResults struct {
	Query   string      `json:"query"`
	Results []searchHit `json:"results"`
}

func (a *app) searchCmd() *cobra.Command {
	var typ, providerName string
	var limit int
	cmd := &cobra.Command{
		Use:   "search [words...]",
		Short: "Search the providers for projects to add",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := checkType(typ); err != nil {
				return err
			}
			if providerName != "" && !slices.Contains(manifest.DefaultProviders, providerName) {
				e := out.Errorf("usage", "--provider takes one of %s, not %q", strings.Join(manifest.DefaultProviders, ", "), providerName)
				e.Candidates, e.Given, e.Flag = manifest.DefaultProviders, providerName, "--provider"
				return e
			}
			if limit < 1 {
				return out.Errorf("usage", "--limit takes a number of results to print, not %d", limit)
			}
			names := manifest.DefaultProviders
			if providerName != "" {
				names = []string{providerName}
			}
			if len(args) == 0 {
				return a.browseSearch(cmd, typ, names, limit)
			}
			query := strings.Join(args, " ")
			reply, err := a.search(cmd.Context(), query, typ, names, limit, true)
			if err != nil {
				return err
			}
			a.warn(reply.warnings)
			return a.printSearch(reply)
		},
	}
	cmd.Flags().StringVar(&typ, "type", "", typeFlagUsage)
	cmd.Flags().StringVar(&providerName, "provider", "", "search one provider instead of every available one")
	cmd.Flags().IntVar(&limit, "limit", 10, "results to print per provider")
	return cmd
}

// searchReply is one query's answer: every hit, the providers that answered, and a warning for
// each that failed while another answered.
type searchReply struct {
	results  searchResults
	searched []string
	warnings []string
}

// search asks each provider in names. A provider that fails becomes a warning and is left out;
// the search fails only when none answered. steps puts each provider's search on its own step
// line, which the live search can't have drawing under its form.
func (a *app) search(ctx context.Context, query, kind string, names []string, limit int, steps bool) (searchReply, error) {
	d, err := a.deps()
	if err != nil {
		return searchReply{}, err
	}
	reply := searchReply{results: searchResults{Query: query, Results: []searchHit{}}}
	var skipped []string
	var failures []error
	for _, name := range names {
		p, ok := d.providers[name]
		if !ok {
			skipped = append(skipped, resolve.Unavailable(name))
			continue
		}
		if steps {
			a.progress("searching %s", name)
		}
		found, err := p.Search(ctx, query, kind, limit)
		if err != nil {
			if steps {
				a.printer.Drop()
			}
			failures = append(failures, err)
			continue
		}
		reply.searched = append(reply.searched, name)
		for _, proj := range found {
			reply.results.Results = append(reply.results.Results, searchHitOf(name, proj))
		}
	}
	if len(reply.searched) > 0 {
		for _, err := range failures {
			reply.warnings = append(reply.warnings, out.AsError(err).Message)
		}
		return reply, nil
	}
	switch {
	case len(failures) > 0:
		return reply, failures[0]
	case len(names) == 1:
		return reply, out.Errorf("provider-unavailable", "%s", skipped[0])
	}
	return reply, out.Errorf("provider-unavailable", "no provider is available: %s", strings.Join(skipped, "; "))
}

func (a *app) printSearch(reply searchReply) error {
	res := reply.results
	return a.printer.Emit(res, func(l *out.Lines) {
		if len(res.Results) == 0 {
			l.Info(noSearchMatches(reply))
			return
		}
		for i, name := range reply.searched {
			items := searchItems(res.Results, name)
			if len(items) == 0 {
				continue
			}
			if i > 0 {
				l.Blank()
			}
			l.Heading(providerTitle(name))
			l.Items(items...)
		}
		l.Nudge("Add one", "shulker add <id>")
	})
}

func noSearchMatches(reply searchReply) string {
	return fmt.Sprintf("No projects match %q on %s.", reply.results.Query, strings.Join(reply.searched, " or "))
}

func searchHitOf(providerName string, p provider.Project) searchHit {
	return searchHit{
		Provider: providerName, ID: p.ID, Slug: p.Slug, Title: p.Title,
		Type: p.Type, Side: p.Side, Downloads: p.Downloads,
	}
}

func searchItems(hits []searchHit, providerName string) []out.Item {
	var items []out.Item
	for _, hit := range hits {
		if hit.Provider != providerName {
			continue
		}
		items = append(items, out.Item{Kind: out.Note, Name: hit.Title, Version: hit.ID, Aside: searchAside(hit)})
	}
	return items
}

func searchAside(hit searchHit) []string {
	var aside []string
	if hit.Type != "" {
		aside = append(aside, hit.Type)
	}
	if hit.Side != "" && hit.Side != "both" {
		aside = append(aside, hit.Side+" only")
	}
	if hit.Downloads > 0 {
		aside = append(aside, downloadCount(hit.Downloads))
	}
	return aside
}

func downloadCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM downloads", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk downloads", float64(n)/1e3)
	case n == 1:
		return "1 download"
	}
	return fmt.Sprintf("%d downloads", n)
}

var providerTitles = map[string]string{"modrinth": "Modrinth", "curseforge": "CurseForge"}

func providerTitle(name string) string {
	if title, ok := providerTitles[name]; ok {
		return title
	}
	return name
}
