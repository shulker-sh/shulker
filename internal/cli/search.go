package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/resolve"
)

// searchResult is one row of a search: a listing, with its hit on each provider it was found on.
type searchResult struct {
	Slug      string      `json:"slug"`
	Title     string      `json:"title"`
	Type      string      `json:"type,omitempty"`
	Side      string      `json:"side,omitempty"`
	Author    string      `json:"author,omitempty"`
	Downloads int64       `json:"downloads,omitempty"`
	Providers []searchHit `json:"providers"`
}

type searchHit struct {
	Provider  string `json:"provider"`
	ID        string `json:"id"`
	Title     string `json:"title"`
	Downloads int64  `json:"downloads,omitempty"`
	Page      string `json:"page,omitempty"`
}

type searchResults struct {
	Query   string         `json:"query"`
	Results []searchResult `json:"results"`
}

func (a *app) searchCmd() *cobra.Command {
	var typ, providerName string
	var limit int
	var verbose bool
	cmd := &cobra.Command{
		Use:         "search [words...]",
		Annotations: reads(),
		Short:       "Search the providers for projects to add",
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
			reply, err := a.search(cmd.Context(), query, typ, names, limit, true, true)
			if err != nil {
				return err
			}
			a.warn(reply.warnings)
			return a.printSearch(reply, verbose)
		},
	}
	cmd.Flags().StringVar(&typ, "type", "", typeFlagUsage)
	cmd.Flags().StringVar(&providerName, "provider", "", "search one provider instead of every available one")
	cmd.Flags().IntVar(&limit, "limit", 10, "results to print per provider")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "also print each provider's id and downloads")
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
// line, which the live search can't have drawing under its form. merge folds each listing found on
// several providers into one row; without it, each provider's hit is a row of its own.
func (a *app) search(ctx context.Context, query, kind string, names []string, limit int, steps, merge bool) (searchReply, error) {
	d, err := a.deps()
	if err != nil {
		return searchReply{}, err
	}
	var step func(name string) func(error)
	if steps {
		step = func(name string) func(error) {
			a.progress("searching %s", a.titles().Title(name))
			return func(err error) {
				if err != nil {
					a.printer.Drop()
				}
			}
		}
	}
	hits, searched, skipped, failures := d.Providers.Search(ctx, names, query, kind, limit, step)
	reply := searchReply{results: searchResults{Query: query, Results: []searchResult{}}, searched: searched}
	var rows []provider.Result
	if merge {
		rows = provider.Merge(hits, a.searchPairs())
	} else {
		for _, hit := range hits {
			rows = append(rows, provider.Result{Hits: []provider.Hit{hit}})
		}
	}
	for _, r := range rows {
		reply.results.Results = append(reply.results.Results, searchResultOf(r))
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
		return reply, skipped[0]
	}
	return reply, resolve.NoneAvailable("no provider is available", skipped)
}

// searchPairs pairs the hits the current project's lock holds as one mod's aliases. Search works
// outside a project, and in one whose lock can't be read, with no pairs.
func (a *app) searchPairs() func(x, y provider.Hit) bool {
	p, err := a.openProject()
	if err != nil || p.Lock == nil {
		return nil
	}
	return func(x, y provider.Hit) bool { return p.Lock.Aliased(x.Provider, x.ID, y.Provider, y.ID) }
}

func (a *app) printSearch(reply searchReply, verbose bool) error {
	res := reply.results
	return a.printer.Emit(res, func(l *out.Lines) {
		if len(res.Results) == 0 {
			l.Info(noSearchMatches(reply))
			return
		}
		l.Blank()
		a.searchTable(l, reply, verbose)
		l.Nudge("Add one", "shulker add <slug>")
	})
}

// searchTable is a row per listing. Source names the providers it was found on, and goes when only
// one was searched; verbose adds each provider's id and splits the downloads between them.
func (a *app) searchTable(l *out.Lines, reply searchReply, verbose bool) {
	t := l.T
	several := len(reply.searched) > 1
	headers := []string{"Name", "Slug", "Type", "Side"}
	styles := []lipgloss.Style{t.StyleBold(), t.Style(), t.StyleGrey(), t.Style()}
	if several {
		headers = append(headers, "Source")
		styles = append(styles, t.StyleGrey())
	}
	if verbose {
		for _, name := range reply.searched {
			title := a.titles().Title(name)
			if !several {
				title = ""
			}
			headers = append(headers, strings.TrimSpace(title+" ID"), strings.TrimSpace(title+" Downloads"))
			styles = append(styles, t.StyleGrey(), t.Style())
		}
	} else {
		headers = append(headers, "Downloads")
		styles = append(styles, t.Style())
	}
	var rows [][]string
	for _, r := range reply.results.Results {
		row := []string{r.Title, r.Slug, r.Type, r.Side}
		if several {
			row = append(row, a.searchSource(r))
		}
		if verbose {
			for _, name := range reply.searched {
				id, downloads := "", ""
				if i := slices.IndexFunc(r.Providers, func(h searchHit) bool { return h.Provider == name }); i >= 0 {
					id, downloads = r.Providers[i].ID, downloadCount(r.Providers[i].Downloads)
				}
				row = append(row, id, downloads)
			}
		} else {
			row = append(row, downloadCount(r.Downloads))
		}
		rows = append(rows, row)
	}
	l.Table(headers, rows, out.Columns(styles...))
}

func (a *app) searchSource(r searchResult) string {
	if len(r.Providers) > 1 {
		return "both"
	}
	return a.titles().Title(r.Providers[0].Provider)
}

func noSearchMatches(reply searchReply) string {
	return fmt.Sprintf("No projects match %q on %s.", reply.results.Query, strings.Join(reply.searched, " or "))
}

func searchResultOf(r provider.Result) searchResult {
	res := searchResult{
		Slug: r.Slug(), Title: r.Title(), Type: r.Type(), Side: r.Side(), Author: r.Author(),
		Downloads: r.Downloads(), Providers: []searchHit{},
	}
	for _, h := range r.Hits {
		res.Providers = append(res.Providers, searchHit{Provider: h.Provider, ID: h.ID, Title: h.Title, Downloads: h.Downloads, Page: h.Page})
	}
	return res
}

func downloadCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	case n == 0:
		return ""
	}
	return fmt.Sprintf("%d", n)
}
