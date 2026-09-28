package cli

import (
	"context"
	"errors"
	"io"
	"shulker.sh/shulker/internal/manifest"
	"slices"
	"strings"
	"testing"
	"time"

	"shulker.sh/shulker/internal/out"
)

type fakeSearches struct {
	s       *liveSearch
	fetched []string
	slept   []time.Duration
	fail    map[string]bool
	// during runs inside the matching fetch or wait, standing in for keys typed meanwhile.
	duringFetch map[string]func()
	duringSleep map[string]func()
}

func newFakeSearches() *fakeSearches {
	f := &fakeSearches{fail: map[string]bool{}, duringFetch: map[string]func(){}, duringSleep: map[string]func(){}}
	f.s = newLiveSearch(out.Theme{}, nil, func(query string) (searchReply, error) {
		f.fetched = append(f.fetched, query)
		if during := f.duringFetch[query]; during != nil {
			during()
		}
		if f.fail[query] {
			return searchReply{}, errors.New("curseforge search " + query + ": 503 Service Unavailable")
		}
		return searchReply{
			results: searchResults{Query: query, Results: []searchResult{{
				Slug: query, Title: "Title " + query, Providers: []searchHit{{Provider: "modrinth", ID: "id-" + query}},
			}}},
			searched: []string{"modrinth"},
		}, nil
	})
	f.s.sleep = func(d time.Duration) {
		f.slept = append(f.slept, d)
		if during := f.duringSleep[f.s.current()]; during != nil {
			during()
		}
	}
	return f
}

func rowValues(rows []out.Choice) []string {
	var values []string
	for _, r := range rows {
		values = append(values, r.Value)
	}
	return values
}

func TestLiveSearchWaitsOutTheDebounce(t *testing.T) {
	f := newFakeSearches()
	f.s.SetQuery("sodium")
	rows := f.s.Rows()
	if !slices.Equal(f.slept, []time.Duration{250 * time.Millisecond}) {
		t.Errorf("slept %v, want one 250ms wait", f.slept)
	}
	if !slices.Equal(rowValues(rows), []string{"modrinth:id-sodium"}) {
		t.Errorf("rows %v", rows)
	}
}

func TestLiveSearchSkipsAQueryTypedPast(t *testing.T) {
	f := newFakeSearches()
	f.duringSleep["sod"] = func() { f.s.SetQuery("sodium") }
	f.s.SetQuery("sod")
	f.s.Rows()
	if len(f.fetched) != 0 {
		t.Errorf("fetched %v while the query was still being typed", f.fetched)
	}
}

func TestLiveSearchNeedsTwoCharacters(t *testing.T) {
	f := newFakeSearches()
	for _, q := range []string{"", "s", " s "} {
		f.s.SetQuery(q)
		if rows := f.s.Rows(); len(rows) != 0 {
			t.Errorf("%q: rows %v", q, rows)
		}
		if status := f.s.Status(); !strings.Contains(status, "two") {
			t.Errorf("%q: status %q", q, status)
		}
	}
	if len(f.fetched) != 0 || len(f.slept) != 0 {
		t.Errorf("fetched %v, slept %v", f.fetched, f.slept)
	}
}

func TestLiveSearchDropsASupersededReply(t *testing.T) {
	f := newFakeSearches()
	f.s.SetQuery("sodium")
	f.s.Rows()
	f.duringFetch["sodiumx"] = func() { f.s.SetQuery("sodiumxy") }
	f.s.SetQuery("sodiumx")
	f.s.Rows()
	if shown := shownQuery(f.s); shown != "sodium" {
		t.Errorf("shown %q, want the sodium reply kept over the one typed past", shown)
	}
}

func TestLiveSearchCachesEachQuery(t *testing.T) {
	f := newFakeSearches()
	for _, q := range []string{"sod", "sodium", "sod"} {
		f.s.SetQuery(q)
		f.s.Rows()
		f.s.Status()
	}
	if !slices.Equal(f.fetched, []string{"sod", "sodium"}) {
		t.Errorf("fetched %v, want each query once", f.fetched)
	}
	if shown := shownQuery(f.s); shown != "sod" {
		t.Errorf("shown %q after backspacing to sod", shown)
	}
}

func TestLiveSearchKeepsTheLastGoodResultsOnFailure(t *testing.T) {
	f := newFakeSearches()
	f.s.SetQuery("sodium")
	f.s.Rows()
	f.fail["sodiumx"] = true
	f.s.SetQuery("sodiumx")
	if rows := f.s.Rows(); !slices.Equal(rowValues(rows), []string{"modrinth:id-sodium"}) {
		t.Errorf("rows %v, want sodium's kept", rows)
	}
	if status := f.s.Status(); !strings.Contains(status, "curseforge search sodiumx: 503") {
		t.Errorf("status %q, want the failure", status)
	}
	if shown := shownQuery(f.s); shown != "sodium" {
		t.Errorf("shown %q", shown)
	}
	f.fail["sodiumx"] = false
	f.s.SetQuery("sodium")
	f.s.SetQuery("sodiumx")
	if rows := f.s.Rows(); !slices.Equal(rowValues(rows), []string{"modrinth:id-sodiumx"}) {
		t.Errorf("rows %v, want a failed query tried again", rows)
	}
}

func TestLiveSearchStatus(t *testing.T) {
	f := newFakeSearches()
	f.s.SetQuery("sodium")
	if status := f.s.Status(); status != "1 result" {
		t.Errorf("status %q", status)
	}
}

func TestLiveSearchForgetsResultsWhenTheQueryIsCleared(t *testing.T) {
	f := newFakeSearches()
	f.s.SetQuery("sodium")
	f.s.Rows()
	f.s.SetQuery("")
	if shown := shownQuery(f.s); shown != "" {
		t.Errorf("a cleared query still shows %q", shown)
	}
}

// shownQuery is the query whose results are on screen, or "" for none.
func shownQuery(l *liveSearch) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.shown == nil {
		return ""
	}
	return l.shown.query
}

func TestSearchRowsAreTheStaticTablesRows(t *testing.T) {
	f := newFakeSearches()
	f.s.SetQuery("sodium")
	answer := searchRows{f.s, true}.Rows()
	if answer.Query != "sodium" || answer.Status != "1 result" || len(answer.Rows) != 1 {
		t.Fatalf("answer %+v", answer)
	}
	if row := answer.Rows[0]; row.Value != "modrinth:id-sodium" || !slices.Equal(row.Cells, []string{"Title sodium", "sodium", "", "", "modrinth", ""}) {
		t.Errorf("row %+v", row)
	}
	f.s.SetQuery("s")
	if answer := (searchRows{f.s, true}).Rows(); len(answer.Rows) != 0 || !strings.Contains(answer.Status, "two") {
		t.Errorf("too short: %+v", answer)
	}
}

func TestSearchDetailsSayWhetherAVersionFits(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	a := h.newApp(io.Discard, io.Discard)
	ctx := context.Background()
	reply, err := a.search(ctx, "sodium", "", manifest.DefaultProviders, 10, false, a.searchMerge(nil))
	if err != nil || len(reply.results.Results) != 1 {
		t.Fatalf("search: %v %+v", err, reply)
	}
	r := reply.results.Results[0]
	p, err := a.openProject()
	if err != nil {
		t.Fatal(err)
	}
	details := a.searchDetails(ctx, p, r, 100)
	for _, want := range []string{
		"Sodium", "mod • client only", "Modrinth", "AANobbMI", "https://modrinth.com/mod/sodium", "228.1M downloads",
		"CurseForge", "394468", "Has a version for Minecraft 26.2 with Fabric",
	} {
		if !strings.Contains(details, want) {
			t.Errorf("missing %q in:\n%s", want, details)
		}
	}
	if details := a.searchDetails(ctx, nil, r, 100); strings.Contains(details, "version for") {
		t.Errorf("outside a project:\n%s", details)
	}
}
