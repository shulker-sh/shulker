package account

import (
	"cmp"
	"slices"
	"strings"
	"unicode/utf8"
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
