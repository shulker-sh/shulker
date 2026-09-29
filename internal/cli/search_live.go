package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/provider"
	"shulker.sh/shulker/internal/resolve"
)

// searchDebounce is how long the query has to sit still before it is searched: one or two
// requests for a phrase typed at a normal pace.
const searchDebounce = 250 * time.Millisecond

// browseSearch is a bare search: a query line over a table of results that follows it, with a
// details view per result. It only reads, so leaving it prints nothing; in a project, a result's
// details can add it, which runs `add` as if it had been named.
func (a *app) browseSearch(cmd *cobra.Command, kind string, names []string, limit int) error {
	if !a.canPick() {
		return checkArgs(cmd, nil, 1, -1)
	}
	if _, err := a.deps(); err != nil {
		return err
	}
	p, err := a.searchProject(cmd)
	if err != nil {
		return err
	}
	merge := a.searchMerge(p)
	a.printer.Settle()
	ctx := cmd.Context()
	s := newLiveSearch(a.printer.ErrTheme, a.titles(), func(query string) (searchReply, error) {
		return a.search(ctx, query, kind, names, limit, false, merge)
	})
	several := len(names) > 1
	headers, styles := searchColumns(a.printer.ErrTheme, several)
	keys := []out.DetailKey{{Key: "o", Help: "open page", Run: func(value string) {
		if r, ok := s.result(value); ok {
			a.openPage(r)
		}
	}}}
	if p != nil {
		keys = append([]out.DetailKey{{Key: "a", Help: "add"}}, keys...)
	}
	key, value, err := a.printer.BrowseTable(out.TableBrowser{
		Title: a.searchTitle(names), Headers: headers, Styles: styles, Source: searchRows{s, several},
		Details: func(value string, width int) string {
			r, ok := s.result(value)
			if !ok {
				return ""
			}
			return a.searchDetails(ctx, p, r, width)
		},
		Keys: keys,
	}, a.stdin)
	if err != nil || key != "a" {
		return err
	}
	r, ok := s.result(value)
	if !ok {
		return nil
	}
	return a.addFound(cmd, r)
}

// addFound runs `add <slug>` for r, from the provider it was found on when only one had it, since
// the same slug can be another project on the other.
func (a *app) addFound(cmd *cobra.Command, r searchResult) error {
	add, _, err := cmd.Root().Find([]string{"add"})
	if err != nil {
		return err
	}
	if r.Type != "" {
		if err := add.Flags().Set("type", r.Type); err != nil {
			return err
		}
	}
	if len(r.Providers) == 1 {
		if err := add.Flags().Set("provider", r.Providers[0].Provider); err != nil {
			return err
		}
	}
	add.SetContext(cmd.Context())
	a.printer.Command = strings.TrimPrefix(add.CommandPath(), "shulker ")
	return add.RunE(add, []string{r.Slug})
}

// openPage opens r's page on the first provider that has one.
func (a *app) openPage(r searchResult) {
	for _, h := range r.Providers {
		if h.Page != "" {
			_ = a.openURL(h.Page)
			return
		}
	}
}

// searchDetails is r's details view: who made it and what it is, its id, downloads and page on
// each provider, and, in project p with a lock, whether it has a version for p's Minecraft and
// loader.
func (a *app) searchDetails(ctx context.Context, p *project.Project, r searchResult, width int) string {
	var b strings.Builder
	l := &out.Lines{W: &b, T: a.printer.ErrTheme}
	t := l.T
	head := t.Bold(r.Title)
	if r.Author != "" {
		head += " " + t.Grey("by "+r.Author)
	}
	l.Plain(head)
	if r.Summary != "" {
		for line := range strings.SplitSeq(ansi.Wordwrap(r.Summary, max(min(width, 100)-4, 20), ""), "\n") {
			l.Plain(line)
		}
	}
	about := []string{r.Type}
	if r.Side == "client" || r.Side == "server" {
		about = append(about, r.Side+" only")
	}
	l.Muted(strings.Join(slices.DeleteFunc(about, func(s string) bool { return s == "" }), " "+t.GlyphDot()+" "))
	l.Blank()
	pad := func(s string, width int) string { return s + strings.Repeat(" ", max(width-out.Width(s), 0)) }
	nameWidth, idWidth := 0, 0
	for _, h := range r.Providers {
		nameWidth, idWidth = max(nameWidth, out.Width(a.titles().Title(h.Provider))), max(idWidth, out.Width(h.ID))
	}
	for _, h := range r.Providers {
		line := t.Bold(pad(a.titles().Title(h.Provider), nameWidth)) + "  " + t.Grey(pad(h.ID, idWidth))
		if h.Downloads > 0 {
			line += "  " + downloadCount(h.Downloads) + " downloads"
		}
		l.Plain(line)
		if h.Page != "" {
			l.Plain(strings.Repeat(" ", nameWidth+2) + h.Page)
		}
	}
	if p != nil && p.Lock != nil {
		l.Blank()
		a.searchFit(ctx, l, p, r)
	}
	return strings.TrimRight(b.String(), "\n")
}

// searchFit says whether r has a version for p's Minecraft and, for what a loader runs, its
// loader: one request, to the first provider r was found on.
func (a *app) searchFit(ctx context.Context, l *out.Lines, p *project.Project, r searchResult) {
	d, err := a.deps()
	if err != nil {
		l.Warn(out.AsError(err).Message)
		return
	}
	res := resolve.NewAt(d.Env, p.Dir)
	res.Manifest, res.Lock = p.Manifest, p.Lock
	platform := "Minecraft " + p.Lock.Minecraft
	if !manifest.IsPackKind(r.Type) && p.Lock.Loader.Type != "" {
		platform += " with " + loader.Title(p.Lock.Loader.Type)
	}
	fits, err := res.Fits(ctx, r.Providers[0].Provider, r.Providers[0].ID, r.Type)
	switch {
	case err != nil:
		l.Warn(out.AsError(err).Message)
	case fits:
		l.OK("Has a version for "+platform, "")
	default:
		l.Warn("No version for " + platform)
	}
}

func (a *app) searchTitle(names []string) string {
	var titles []string
	for _, name := range names {
		if p, err := a.d.Providers.Get(name); err == nil {
			titles = append(titles, p.Title())
		}
	}
	if len(titles) == 0 {
		return "Search"
	}
	return "Search " + strings.Join(titles, " and ")
}

// liveSearch answers the bare search's form. Every change to the query asks for rows and a
// status line, each on its own goroutine; both wait out the debounce, then share one request
// per query, which is kept for the session.
type liveSearch struct {
	theme out.Theme
	fetch func(query string) (searchReply, error)
	sleep func(time.Duration)
	// titles names each hit's provider as the user reads it.
	titles provider.Providers

	// fetching holds one request at a time: a slow one makes the next wait rather than race it,
	// and a query typed past while waiting is never sent.
	fetching sync.Mutex

	mu      sync.Mutex
	query   string
	replies map[string]*liveReply
	// shown is the reply on screen: the latest one that answered while its query was current.
	shown *liveReply
}

type liveReply struct {
	query        string
	done         chan struct{}
	reply        searchReply
	err          error
	isSuperseded bool
}

func (l *liveReply) hasFailed() bool {
	select {
	case <-l.done:
		return l.err != nil
	default:
		return false
	}
}

func newLiveSearch(theme out.Theme, titles provider.Providers, fetch func(query string) (searchReply, error)) *liveSearch {
	return &liveSearch{theme: theme, titles: titles, fetch: fetch, sleep: time.Sleep, replies: map[string]*liveReply{}}
}

// SetQuery takes the query as typed. Moving off a query forgets every failed reply: the rows
// and status asked for one query share its failure, and a later ask for it tries again. huh
// keeps what it was given per exact text, so that later ask only comes for the same query typed
// differently, like with a trailing space.
func (l *liveSearch) SetQuery(query string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	query = strings.TrimSpace(query)
	if query != l.query {
		for q, r := range l.replies {
			if r.hasFailed() {
				delete(l.replies, q)
			}
		}
	}
	l.query = query
	if isTooShort(query) {
		l.shown = nil
	}
}

func (l *liveSearch) current() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.query
}

// isTooShort is a query not worth a request: one letter matches a near-arbitrary slice of
// everything, and it is the query most likely to be typed straight through.
func isTooShort(query string) bool { return len([]rune(query)) < 2 }

func (l *liveSearch) Rows() []out.Choice {
	l.settle()
	l.mu.Lock()
	defer l.mu.Unlock()
	if isTooShort(l.query) || l.shown == nil {
		return nil
	}
	t := l.theme
	var rows []out.Choice
	for _, r := range l.shown.reply.results.Results {
		var aside []string
		for _, h := range r.Providers {
			aside = append(aside, l.titles.Title(h.Provider))
		}
		if r.Type != "" {
			aside = append(aside, r.Type)
		}
		if r.Downloads > 0 {
			aside = append(aside, downloadCount(r.Downloads)+" downloads")
		}
		rows = append(rows, out.Choice{
			Label: t.Bold(r.Title) + " " + t.Grey(r.Slug) + t.Aside(strings.Join(aside, ", ")),
			Value: searchValue(r),
		})
	}
	return rows
}

func (l *liveSearch) Status() string { return l.statusOf(l.settle()) }

func (l *liveSearch) statusOf(r *liveReply) string {
	t := l.theme
	switch {
	case r == nil:
		if isTooShort(l.current()) {
			return t.Grey("Type two or more characters to search.")
		}
		return ""
	case r.err != nil:
		return t.Red(t.GlyphError() + " " + out.AsError(r.err).Message)
	case len(r.reply.warnings) > 0:
		return t.Yellow("! " + strings.Join(r.reply.warnings, "; "))
	case len(r.reply.results.Results) == 0:
		return noSearchMatches(r.reply)
	case len(r.reply.results.Results) == 1:
		return "1 result"
	}
	return fmt.Sprintf("%d results", len(r.reply.results.Results))
}

// result is the result on screen whose row has value.
func (l *liveSearch) result(value string) (searchResult, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shown == nil {
		return searchResult{}, false
	}
	for _, r := range l.shown.reply.results.Results {
		if searchValue(r) == value {
			return r, true
		}
	}
	return searchResult{}, false
}

// searchValue is what a result's row means: its first provider and its id there, which no other
// result shares.
func searchValue(r searchResult) string { return r.Providers[0].Provider + ":" + r.Providers[0].ID }

// searchRows is a live search as the table browser reads it: the rows on screen, as the static
// table draws them, and the status line.
type searchRows struct {
	*liveSearch
	several bool
}

func (s searchRows) Rows() out.TableAnswer {
	r := s.settle()
	answer := out.TableAnswer{Status: s.statusOf(r)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if isTooShort(s.query) || s.shown == nil {
		return answer
	}
	answer.Query = s.shown.query
	for _, res := range s.shown.reply.results.Results {
		answer.Rows = append(answer.Rows, out.TableRow{Cells: searchRow(s.titles, res, s.several), Value: searchValue(res)})
	}
	return answer
}

// settle waits out the debounce for the query as it stands, then answers it from the session's
// replies or a new request. It returns nil for a query too short to search and for one typed
// past before its answer came: huh drops a reply to a query that is no longer current, and
// shown must not take it either.
func (l *liveSearch) settle() *liveReply {
	query := l.current()
	if isTooShort(query) {
		return nil
	}
	l.sleep(searchDebounce)
	if l.current() != query {
		return nil
	}
	l.mu.Lock()
	r, found := l.replies[query]
	if !found {
		r = &liveReply{query: query, done: make(chan struct{})}
		l.replies[query] = r
	}
	l.mu.Unlock()
	if found {
		<-r.done
	} else {
		l.answer(r)
	}
	if r.isSuperseded {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if r.err == nil && l.query == r.query {
		l.shown = r
	}
	return r
}

// answer sends r's request, unless its query was typed past while an earlier request held the
// line; an unsent one is forgotten.
func (l *liveSearch) answer(r *liveReply) {
	l.fetching.Lock()
	if l.current() == r.query {
		r.reply, r.err = l.fetch(r.query)
	} else {
		r.isSuperseded = true
	}
	l.fetching.Unlock()
	if r.isSuperseded {
		l.mu.Lock()
		delete(l.replies, r.query)
		l.mu.Unlock()
	}
	close(r.done)
}
