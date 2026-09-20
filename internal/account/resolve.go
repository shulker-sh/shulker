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

// DefaultProviders is accounts.providers when config.json doesn't set it.
func DefaultProviders() []string { return []string{SourceShulker} }

// Providers is every name accounts.providers accepts, in the order they are offered. A launcher is
// listed here before its reader exists, so a list can drop shulker for one of them; WithoutReader
// says which of those a run can't act on yet.
func Providers() []string { return []string{SourceShulker, "prism", "mojang"} }

// WithoutReader is the configured providers shulker has no reader for, so a run can say why it
// found nothing there instead of listing nothing and leaving the player to guess.
func WithoutReader(providers []string) []string {
	var missing []string
	for _, p := range providers {
		if p != SourceShulker {
			missing = append(missing, p)
		}
	}
	return missing
}

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

// Resolve is every account the providers yield, in provider order. One Microsoft account can sit in
// several launchers, so the list is deduped by id and the earliest provider wins.
func Resolve(providers []string, own Store) []Resolved {
	var out []Resolved
	seen := map[string]bool{}
	for _, p := range providers {
		for _, r := range fromProvider(p, own) {
			if r.ID == "" || seen[normalizeID(r.ID)] {
				continue
			}
			seen[normalizeID(r.ID)] = true
			out = append(out, r)
		}
	}
	return out
}

// fromProvider is what one provider contributes. Only shulker's own file has a reader today; the
// launchers stay silent, which is what a provider with nothing to offer does.
func fromProvider(provider string, own Store) []Resolved {
	if provider != SourceShulker {
		return nil
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
	slices.SortStableFunc(accounts, func(a, b Resolved) int {
		if c := strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
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
