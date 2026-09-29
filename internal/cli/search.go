package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
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
	Summary   string      `json:"summary,omitempty"`
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
			p, err := a.searchProject(cmd)
			if err != nil {
				return err
			}
			reply, err := a.search(cmd.Context(), query, typ, names, limit, true, a.searchMerge(p))
			if err != nil {
				return err
			}
			a.warn(reply.warnings)
			return a.printSearch(reply, verbose)
		},
	}
	a.scopeFlags(cmd)
	cmd.Flags().StringVar(&typ, "type", "", typeFlagUsage)
	cmd.Flags().StringVar(&providerName, "provider", "", "search one provider instead of every available one.")
	cmd.Flags().IntVar(&limit, "limit", 10, "results to print per provider")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "also print each provider's id and downloads.")
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
// line, which the live search can't have drawing under its form. merge makes the hits into rows.
func (a *app) search(ctx context.Context, query, kind string, names []string, limit int, steps bool, merge func([]provider.Hit) []provider.Result) (searchReply, error) {
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
	for _, r := range merge(hits) {
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

// searchProject is the project a search runs in, or nil: search works outside a project, and in
// one that can't be read. A project named by -C or -i has to open.
func (a *app) searchProject(cmd *cobra.Command) (*project.Project, error) {
	p, err := a.openProject()
	if err != nil && !cmd.Flags().Changed("dir") && !cmd.Flags().Changed("instance") {
		return nil, nil
	}
	return p, err
}

// searchMerge folds each listing found on several providers into one row, pairing too the hits
// p's lock holds as one mod's aliases and those the listing index proved. It reads p and the index
// once, so the live search can call it off the drawing loop.
func (a *app) searchMerge(p *project.Project) func([]provider.Hit) []provider.Result {
	var l *lock.Lock
	if p != nil {
		l = p.Lock
	}
	d, err := a.deps()
	if err != nil {
		return func(hits []provider.Hit) []provider.Result { return provider.Merge(hits, nil) }
	}
	ix, err := d.Cache.ReadListings()
	if err != nil {
		a.printer.Warn("the listing index wasn't read (%s).", err)
	}
	var mu sync.Mutex
	touched := map[[2]cache.Listing]bool{}
	return func(hits []provider.Hit) []provider.Result {
		mu.Lock()
		defer mu.Unlock()
		var used [][2]cache.Listing
		paired := func(x, y provider.Hit) bool {
			if l != nil && l.Aliased(x.Provider, x.ID, y.Provider, y.ID) {
				return true
			}
			pair := [2]cache.Listing{{Provider: x.Provider, ID: x.ID}, {Provider: y.Provider, ID: y.ID}}
			if !ix.Paired(pair[0], pair[1]) {
				return false
			}
			if !touched[pair] && ix.Stale(pair[0], pair[1]) {
				touched[pair] = true
				used = append(used, pair)
			}
			return true
		}
		results := provider.Merge(hits, paired)
		// A pair whose use isn't recorded is pruned sooner, which costs one jar fetch later: not
		// worth failing or interrupting a search over.
		if len(used) > 0 {
			_ = d.Cache.UseListings(used...)
		}
		return results
	}
}

// searchUnmerged is each provider's hit as a row of its own.
func searchUnmerged(hits []provider.Hit) []provider.Result {
	rows := make([]provider.Result, 0, len(hits))
	for _, hit := range hits {
		rows = append(rows, provider.Result{Hits: []provider.Hit{hit}})
	}
	return rows
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

// searchTable is a row per listing; verbose adds each provider's id and splits the downloads
// between them.
func (a *app) searchTable(l *out.Lines, reply searchReply, verbose bool) {
	t := l.T
	several := len(reply.searched) > 1
	headers, styles := searchColumns(t, several)
	if verbose {
		headers, styles = headers[:len(headers)-1], styles[:len(styles)-1]
		for _, name := range reply.searched {
			title := a.titles().Title(name) + " "
			if !several {
				title = ""
			}
			headers = append(headers, title+"ID", title+"Downloads")
			styles = append(styles, t.StyleGrey(), t.Style())
		}
	}
	var rows [][]string
	for _, r := range reply.results.Results {
		row := searchRow(a.titles(), r, several)
		if verbose {
			row = row[:len(row)-1]
			for _, name := range reply.searched {
				id, downloads := "", ""
				if i := slices.IndexFunc(r.Providers, func(h searchHit) bool { return h.Provider == name }); i >= 0 {
					id, downloads = r.Providers[i].ID, downloadCount(r.Providers[i].Downloads)
				}
				row = append(row, id, downloads)
			}
		}
		rows = append(rows, row)
	}
	l.Table(headers, rows, out.Columns(styles...))
}

// searchColumns are a search table's headers and styles, the static one's and the live one's
// alike. Source names the providers a row was found on, so it goes when only one was searched.
func searchColumns(t out.Theme, several bool) ([]string, []lipgloss.Style) {
	headers := []string{"Name", "Slug", "Type", "Side"}
	styles := []lipgloss.Style{t.StyleBold(), t.Style(), t.StyleGrey(), t.Style()}
	if several {
		headers = append(headers, "Source")
		styles = append(styles, t.StyleGrey())
	}
	return append(headers, "Downloads"), append(styles, t.Style())
}

func searchRow(titles provider.Providers, r searchResult, several bool) []string {
	row := []string{r.Title, r.Slug, r.Type, r.Side}
	if several {
		source := "both"
		if len(r.Providers) == 1 {
			source = titles.Title(r.Providers[0].Provider)
		}
		row = append(row, source)
	}
	return append(row, downloadCount(r.Downloads))
}

func noSearchMatches(reply searchReply) string {
	return fmt.Sprintf("No projects match %q on %s.", reply.results.Query, strings.Join(reply.searched, " or "))
}

func searchResultOf(r provider.Result) searchResult {
	res := searchResult{
		Slug: r.Slug(), Title: r.Title(), Type: r.Type(), Side: r.Side(), Author: r.Author(), Summary: r.Summary(),
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
