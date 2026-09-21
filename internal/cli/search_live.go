package cli

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/out"
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
	s := newLiveSearch(a.printer.ErrTheme, func(query string) (searchReply, error) {
		return a.search(ctx, query, kind, names, limit, false)
	})
	if err := a.printer.Browse(a.searchTitle(names), s, a.stdin); err != nil {
		return err
	}
	reply, ok := s.last()
	if !ok {
		return nil
	}
	a.warn(reply.warnings)
	return a.printSearch(reply)
}

func (a *app) searchTitle(names []string) string {
	var titles []string
	for _, name := range names {
		if _, ok := a.d.providers[name]; ok {
			titles = append(titles, providerTitle(name))
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
	query      string
	done       chan struct{}
	reply      searchReply
	err        error
	superseded bool
}

func (r *liveReply) failed() bool {
	select {
	case <-r.done:
		return r.err != nil
	default:
		return false
	}
}

func newLiveSearch(theme out.Theme, fetch func(query string) (searchReply, error)) *liveSearch {
	return &liveSearch{theme: theme, fetch: fetch, sleep: time.Sleep, replies: map[string]*liveReply{}}
}

// SetQuery takes the query as typed. Moving off a query forgets every failed reply: the rows
// and status asked for one query share its failure, and a later ask for it tries again. huh
// keeps what it was given per exact text, so that later ask only comes for the same query typed
// differently, like with a trailing space.
func (s *liveSearch) SetQuery(query string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	query = strings.TrimSpace(query)
	if query != s.query {
		for q, r := range s.replies {
			if r.failed() {
				delete(s.replies, q)
			}
		}
	}
	s.query = query
	if tooShort(query) {
		s.shown = nil
	}
}

func (s *liveSearch) current() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.query
}

// tooShort is a query not worth a request: one letter matches a near-arbitrary slice of
// everything, and it is the query most likely to be typed straight through.
func tooShort(query string) bool { return len([]rune(query)) < 2 }

func (s *liveSearch) Rows() []out.Choice {
	s.settle()
	s.mu.Lock()
	defer s.mu.Unlock()
	if tooShort(s.query) || s.shown == nil {
		return nil
	}
	t := s.theme
	var rows []out.Choice
	for _, hit := range s.shown.reply.results.Results {
		aside := append([]string{providerTitle(hit.Provider)}, searchAside(hit)...)
		rows = append(rows, out.Choice{
			Label: t.Bold(hit.Title) + " " + t.Grey(hit.ID) + t.Aside(strings.Join(aside, ", ")),
			Value: hit.Provider + ":" + hit.ID,
		})
	}
	return rows
}

func (s *liveSearch) Status() string {
	r := s.settle()
	t := s.theme
	switch {
	case r == nil:
		if tooShort(s.current()) {
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
func (s *liveSearch) last() (searchReply, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shown == nil {
		return searchReply{}, false
	}
	return s.shown.reply, true
}

// settle waits out the debounce for the query as it stands, then answers it from the session's
// replies or a new request. It returns nil for a query too short to search and for one typed
// past before its answer came: huh drops a reply to a query that is no longer current, and
// shown must not take it either.
func (s *liveSearch) settle() *liveReply {
	query := s.current()
	if tooShort(query) {
		return nil
	}
	s.sleep(searchDebounce)
	if s.current() != query {
		return nil
	}
	s.mu.Lock()
	r, found := s.replies[query]
	if !found {
		r = &liveReply{query: query, done: make(chan struct{})}
		s.replies[query] = r
	}
	s.mu.Unlock()
	if found {
		<-r.done
	} else {
		s.answer(r)
	}
	if r.superseded {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.err == nil && s.query == r.query {
		s.shown = r
	}
	return r
}

// answer sends r's request, unless its query was typed past while an earlier request held the
// line; an unsent one is forgotten.
func (s *liveSearch) answer(r *liveReply) {
	s.fetching.Lock()
	if s.current() == r.query {
		r.reply, r.err = s.fetch(r.query)
	} else {
		r.superseded = true
	}
	s.fetching.Unlock()
	if r.superseded {
		s.mu.Lock()
		delete(s.replies, r.query)
		s.mu.Unlock()
	}
	close(r.done)
}
