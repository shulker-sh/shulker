package account

import "strings"

// Find is every account a selector names: a username matched case-insensitively, a UUID dashed or
// not, or either of those qualified with @source. A name may hold spaces, since an account with no
// Java profile is named by its Xbox gamertag, so a selector is quoted on the command line.
func Find(accounts []Resolved, query string) []Resolved {
	name, source := split(query)
	id := normalizeID(name)
	var matches []Resolved
	for _, a := range accounts {
		if source != "" && !strings.EqualFold(a.Source, source) {
			continue
		}
		if (id != "" && normalizeID(a.ID) == id) || (name != "" && strings.EqualFold(a.Name, name)) {
			matches = append(matches, a)
		}
	}
	Sort(matches)
	return matches
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
func normalizeID(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "-", ""))
}

// SameID reports whether two ids name one account, reading each the way it was typed.
func SameID(a, b string) bool {
	return a != "" && normalizeID(a) == normalizeID(b)
}
