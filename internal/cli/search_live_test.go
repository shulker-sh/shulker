package cli

import (
	"errors"
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
	if reply, ok := f.s.last(); !ok || reply.results.Query != "sodium" {
		t.Errorf("last %+v %v, want the sodium reply kept over the one typed past", reply, ok)
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
	if reply, _ := f.s.last(); reply.results.Query != "sod" {
		t.Errorf("last %q after backspacing to sod", reply.results.Query)
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
	if reply, _ := f.s.last(); reply.results.Query != "sodium" {
		t.Errorf("last %q", reply.results.Query)
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
	if _, ok := f.s.last(); ok {
		t.Error("a cleared query still has results to print")
	}
}
