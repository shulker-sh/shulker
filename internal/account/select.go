package account

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"

	"shulker.sh/shulker/internal/out"
)

// How a selector matched an account, best first.
const (
	byID = iota
	byName
	byNameFolded
	byPrefix
	byPrefixFolded
	byIDPrefix
	noMatch
)

// Find is every account a selector names, closest first. A selector is a username or a prefix of
// one, matched with case first and without it second, or a UUID or a prefix of one, dashed or not,
// and either may be qualified with @source. A name may hold spaces, since an account with no Java
// profile is named by its Xbox gamertag, so a selector is quoted on the command line.
//
// With several matches, closest reports that the first is the answer. Without it the input can't
// tell the matches apart, and only those it can't tell apart are returned: accounts with the very
// same name, or ids sharing the prefix typed.
func Find(accounts []Resolved, query string) (matches []Resolved, closest bool) {
	type match struct {
		Resolved
		rank int
	}
	name, source := split(query)
	var found []match
	best := noMatch
	for _, a := range accounts {
		if source != "" && !strings.EqualFold(a.Source, source) {
			continue
		}
		if r := rank(a, name); r != noMatch {
			found = append(found, match{a, r})
			best = min(best, r)
		}
	}
	// An id prefix counts only when no name starts with the input.
	found = slices.DeleteFunc(found, func(m match) bool { return best != byIDPrefix && m.rank == byIDPrefix })
	slices.SortStableFunc(found, func(a, b match) int {
		return cmp.Or(cmp.Compare(a.rank, b.rank),
			cmp.Compare(utf8.RuneCountInString(a.Name), utf8.RuneCountInString(b.Name)),
			compare(a.Resolved, b.Resolved))
	})
	var tied []Resolved
	for _, m := range found {
		matches = append(matches, m.Resolved)
		if m.rank == best && (best == byIDPrefix || m.Name == found[0].Name) {
			tied = append(tied, m.Resolved)
		}
	}
	if len(tied) > 1 {
		return tied, false
	}
	return matches, len(matches) > 1
}

func rank(a Resolved, name string) int {
	if name == "" {
		return noMatch
	}
	id := NormalizeID(name)
	switch {
	case SameID(a.ID, name):
		return byID
	case a.Name == name:
		return byName
	case strings.EqualFold(a.Name, name):
		return byNameFolded
	case strings.HasPrefix(a.Name, name):
		return byPrefix
	case strings.HasPrefix(strings.ToLower(a.Name), strings.ToLower(name)):
		return byPrefixFolded
	case id != "" && strings.HasPrefix(NormalizeID(a.ID), id):
		return byIDPrefix
	}
	return noMatch
}

// split takes the @source off a selector. The last @ wins, and a leading one is part of the name,
// so an account somehow called "@steve" still selects.
func split(query string) (name, source string) {
	if at := strings.LastIndex(query, "@"); at > 0 {
		return query[:at], query[at+1:]
	}
	return query, ""
}

// normalizeID reads an id the way it is typed: a UUID dashed or not, in either case.
// NormalizeID reads an id the way it was typed, dashed or not, in either case.
func NormalizeID(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "-", ""))
}

// SameID reports whether two ids name one account, reading each the way it was typed.
func SameID(a, b string) bool {
	return a != "" && NormalizeID(a) == NormalizeID(b)
}

// Picker asks which of several accounts a selector meant; false is a question that was escaped.
type Picker func(matches []Resolved) (Resolved, bool, error)

// Select resolves what an account argument names. One match is the answer, and so is the closest
// of several, with a warning; several the input can't tell apart go to the picker, and are
// ambiguous-account with no picker or an escaped one, since the matches and how to name one
// without being asked again are what the caller needs either way.
func Select(accounts []Resolved, query string, warn func(format string, args ...any), pick Picker) (Resolved, error) {
	if len(accounts) == 0 {
		return Resolved{}, NoAccounts()
	}
	matches, closest := Find(accounts, query)
	switch {
	case len(matches) == 0:
		e := out.Errorf("account-not-found", "no account matches %q", query)
		e.Candidates, e.Pass, e.Given = Candidates(accounts), Picks(accounts), query
		return Resolved{}, e
	case closest:
		warn("auto-selecting %s (%s), the closest match to %q", matches[0].Name, matches[0].ID, query)
		return matches[0], nil
	case len(matches) == 1:
		return matches[0], nil
	}
	ambiguous := func() error {
		e := out.Errorf("ambiguous-account", "%d accounts match %q", len(matches), query)
		e.Help = "name one by its qualifier or its id"
		e.Candidates, e.Pass, e.Given = Candidates(matches), Picks(matches), query
		return e
	}
	if pick == nil {
		return Resolved{}, ambiguous()
	}
	r, ok, err := pick(matches)
	if err != nil {
		return Resolved{}, err
	}
	if !ok {
		return Resolved{}, ambiguous()
	}
	return r, nil
}

// NoAccounts is what a command that needs an account says when shulker can see none.
func NoAccounts() error {
	e := out.Errorf("no-accounts", "shulker can see no accounts, so there is nothing to play with")
	e.Nudge = out.Nudge{Lead: "Sign in to Microsoft", Command: "shulker accounts login"}
	return e
}

// Candidates names each account the way a selector would: its qualifier, then its id, since two
// accounts may share a name and only the id always tells them apart.
func Candidates(accounts []Resolved) []string {
	names := make([]string, len(accounts))
	for i, r := range accounts {
		names[i] = r.Qualifier() + " — " + r.ID
	}
	return names
}

// Picks is the id of each account, which is what an error's pass list offers.
func Picks(accounts []Resolved) []string {
	picks := make([]string, len(accounts))
	for i, r := range accounts {
		picks[i] = r.ID
	}
	return picks
}

// Removable is the accounts `accounts remove` deletes: the offline ones.
func Removable(accounts []Resolved) []Resolved {
	return slices.DeleteFunc(slices.Clone(accounts), func(r Resolved) bool { return r.Source != SourceOffline })
}

// Selectors names each of some accounts the shortest way that picks it out of all of them: its
// name when no other account shares it, its id when one does.
func Selectors(all, some []Resolved) []string {
	names := make([]string, len(some))
	for i, r := range some {
		shared := slices.ContainsFunc(all, func(o Resolved) bool { return o.ID != r.ID && strings.EqualFold(o.Name, r.Name) })
		if shared {
			names[i] = r.ID
		} else {
			names[i] = QuoteName(r.Name)
		}
	}
	return names
}
