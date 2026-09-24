package account

import (
	"slices"
	"strings"
	"time"
)

// The sources a selector names after @. An account shulker signed in itself is @shulker and one it
// created is @offline, both out of its own accounts.json; a borrowed account takes the name of the
// launcher it came from.
const (
	SourceShulker = "shulker"
	SourceOffline = "offline"
)

// DefaultStores is accounts.stores when config.json doesn't set it.
func DefaultStores() []string { return []string{SourceShulker} }

// Group is which block of the account list an account prints under.
type Group string

const (
	GroupOwn      Group = "own"
	GroupOffline  Group = "offline"
	GroupBorrowed Group = "borrowed"
)

// Resolved is one account as every command that names one sees it.
type Resolved struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Source string `json:"source"`
	Group  Group  `json:"group"`
	State  State  `json:"state"`
	// Expired is when a borrowed session token ran out, which the state line dates.
	Expired time.Time `json:"-"`
	Account Account   `json:"-"`
}

// Qualifier is how a selector names this account when its name alone is ambiguous.
func (r Resolved) Qualifier() string { return r.Name + "@" + r.Source }

// Resolve is every account the stores yield, in store order: shulker's own file, and what each
// launcher's reader already took from it. One Microsoft account can sit in several launchers, so
// the list is deduped by id and the earliest store wins.
func Resolve(stores []string, own Store, borrowed map[string][]Resolved) []Resolved {
	var out []Resolved
	seen := map[string]bool{}
	for _, p := range stores {
		for _, r := range fromStore(p, own, borrowed) {
			if r.ID == "" || seen[NormalizeID(r.ID)] {
				continue
			}
			seen[NormalizeID(r.ID)] = true
			out = append(out, r)
		}
	}
	return out
}

// fromStore is what one store contributes. A launcher's accounts are read where they live, before
// this, and a launcher with nothing to offer yields nothing.
func fromStore(store string, own Store, borrowed map[string][]Resolved) []Resolved {
	if store != SourceShulker {
		return borrowed[store]
	}
	out := make([]Resolved, 0, len(own.Accounts))
	for _, a := range own.Accounts {
		r := Resolved{ID: a.ID(), Name: a.Name(), Source: SourceShulker, Group: GroupOwn, State: a.State(), Account: a}
		if a.Type == Offline {
			r.Source, r.Group = SourceOffline, GroupOffline
		}
		out = append(out, r)
	}
	return out
}

// Sort orders a group's rows the way the list prints them: by name, and by id where two accounts
// share one, so the order never depends on how the file happened to be written.
func Sort(accounts []Resolved) {
	slices.SortStableFunc(accounts, compare)
}

func compare(a, b Resolved) int {
	if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
		return c
	}
	return strings.Compare(a.ID, b.ID)
}

// InGroup is the accounts of one group, sorted.
func InGroup(accounts []Resolved, g Group) []Resolved {
	var out []Resolved
	for _, a := range accounts {
		if a.Group == g {
			out = append(out, a)
		}
	}
	Sort(out)
	return out
}
