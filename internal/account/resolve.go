package account

import (
	"slices"
	"strings"
	"time"
)

// The sources a selector names after @. An account shulker signed in itself is @shulker and one it
// created is @offline, both out of its own accounts.json; a launcher account takes the name of the
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
	GroupLauncher Group = "launcher"
)

// Resolved is one account as every command that names one sees it.
type Resolved struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Source string `json:"source"`
	Group  Group  `json:"group"`
	State  State  `json:"state"`
	// Expired is when a launcher account's session token ran out, which the state line dates.
	Expired time.Time `json:"-"`
	Account Account   `json:"-"`
}

// Qualifier is how a selector names this account when its name alone is ambiguous.
func (r Resolved) Qualifier() string { return r.Name + "@" + r.Source }

// Launchable is the accounts a launch could use: every state that doesn't stop one, which is the
// same test `accounts logout` uses to hand the default on.
func Launchable(accounts []Resolved) []Resolved {
	var usable []Resolved
	for _, r := range accounts {
		if r.State.Error(r.Name) == nil {
			usable = append(usable, r)
		}
	}
	return usable
}

// Resolve is every account the stores yield, in store order: shulker's own file, and what each
// launcher's reader already took from it. One Microsoft account can sit in several launchers, so
// the list is deduped by id and the earliest store wins.
func Resolve(stores []string, own Store, fromLaunchers map[string][]Resolved) []Resolved {
	var out []Resolved
	seen := map[string]bool{}
	for _, p := range stores {
		for _, r := range fromStore(p, own, fromLaunchers) {
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
func fromStore(store string, own Store, fromLaunchers map[string][]Resolved) []Resolved {
	if store != SourceShulker {
		return fromLaunchers[store]
	}
	out := make([]Resolved, 0, len(own.Accounts))
	for _, a := range own.Accounts {
		out = append(out, a.Resolved())
	}
	return out
}

// Resolved is the account as a command sees it: one of shulker's own, or an offline one by its type.
func (a Account) Resolved() Resolved {
	r := Resolved{ID: a.ID(), Name: a.Name(), Source: SourceShulker, Group: GroupOwn, State: a.State(), Account: a}
	if a.Type == Offline {
		r.Source, r.Group = SourceOffline, GroupOffline
	}
	return r
}

// IsOwn reports an account shulker signed in itself, which is the only kind it can renew or log out.
func (r Resolved) IsOwn() bool { return r.Source == SourceShulker }

// IsDefault reports whether this account is the one accounts.default names.
func (r Resolved) IsDefault(id string) bool { return id != "" && SameID(r.ID, id) }

// Own keeps the accounts shulker signed in itself.
func Own(accounts []Resolved) []Resolved {
	var own []Resolved
	for _, r := range accounts {
		if r.IsOwn() {
			own = append(own, r)
		}
	}
	return own
}

// WithoutProfile keeps the accounts that own no Java profile.
func WithoutProfile(accounts []Resolved) []Resolved {
	var kept []Resolved
	for _, r := range accounts {
		if r.State == NoProfile {
			kept = append(kept, r)
		}
	}
	return kept
}

// ByID is the account with exactly this id, as the registry or an instance file records it.
func ByID(accounts []Resolved, id string) (Resolved, bool) {
	for _, r := range accounts {
		if r.ID == id {
			return r, true
		}
	}
	return Resolved{}, false
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

// Successor is who inherits the default when the account holding it goes: the single account a
// launch could still use, else nobody. An offline account inherits as readily as a signed-in one,
// since it launches as readily; what can't is what a launch refuses.
func Successor(accounts []Resolved) (Resolved, bool) {
	usable := Launchable(accounts)
	if len(usable) != 1 {
		return Resolved{}, false
	}
	return usable[0], true
}
