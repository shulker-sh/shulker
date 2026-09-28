package cli

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

// searchDebounce is how long the query has to sit still before it is searched: one or two
// requests for a phrase typed at a normal pace.
const searchDebounce = 250 * time.Millisecond

// browseSearch is a bare search: a query line over results that follow it, which only reads, so
// leaving it prints the results on screen as `shulker search <that query>` would have.
func (a *app) browseSearch(cmd *cobra.Command, kind string, names []string, limit int) error {
	if !a.canPick() {
		return checkArgs(cmd, nil, 1, -1)
	}
	if _, err := a.deps(); err != nil {
		return err
	}
	a.printer.Settle()
	ctx := cmd.Context()
	s := newLiveSearch(a.printer.ErrTheme, a.titles(), func(query string) (searchReply, error) {
		return a.search(ctx, query, kind, names, limit, false, true)
	})
	if err := a.printer.Browse(a.searchTitle(names), s, a.stdin); err != nil {
		return err
	}
	reply, ok := s.last()
	if !ok {
		return nil
	}
	a.warn(reply.warnings)
	return a.printSearch(reply, false)
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
		aside = append(aside, r.Type)
		if r.Downloads > 0 {
			aside = append(aside, downloadCount(r.Downloads)+" downloads")
		}
		rows = append(rows, out.Choice{
			Label: t.Bold(r.Title) + " " + t.Grey(r.Slug) + t.Aside(strings.Join(aside, ", ")),
			Value: r.Providers[0].Provider + ":" + r.Providers[0].ID,
		})
	}
	return rows
}

func (l *liveSearch) Status() string {
	r := l.settle()
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

// last is the reply on screen, which leaving the form prints.
func (l *liveSearch) last() (searchReply, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shown == nil {
		return searchReply{}, false
	}
	return l.shown.reply, true
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
