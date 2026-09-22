package cli

import (
	"time"

	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/out"
)

// selectAccount resolves what an account argument names. One match is the answer, and so is the
// closest of several, with a warning; several the input can't tell apart get a picker on a
// terminal, and ambiguous-account anywhere else.
func (a *app) selectAccount(query string) (account.Resolved, error) {
	accounts, _, err := a.accounts()
	if err != nil {
		return account.Resolved{}, err
	}
	if len(accounts) == 0 {
		e := out.Errorf("no-accounts", "shulker can see no accounts, so there is nothing to play with")
		e.Nudge = out.Nudge{Lead: "Sign in to Microsoft", Command: "shulker accounts login"}
		return account.Resolved{}, e
	}
	matches, closest := account.Find(accounts, query)
	switch {
	case len(matches) == 0:
		e := out.Errorf("account-not-found", "no account matches %q", query)
		e.Candidates, e.Pass, e.Given = accountCandidates(accounts), accountPicks(accounts), query
		return account.Resolved{}, e
	case closest:
		a.printer.Warn("auto-selecting %s (%s), the closest match to %q", matches[0].Name, matches[0].ID, query)
		return matches[0], nil
	case len(matches) == 1:
		return matches[0], nil
	}
	return a.pickAccount(query, matches)
}

func (a *app) pickAccount(query string, matches []account.Resolved) (account.Resolved, error) {
	t, now := a.printer.ErrTheme, time.Now()
	return pickOne(a, "Which account?", matches,
		func(r account.Resolved) string { return r.ID },
		func(r account.Resolved) string { return accountLabel(t, r, now) },
		func() error {
			e := out.Errorf("ambiguous-account", "%d accounts match %q", len(matches), query)
			e.Help = "name one by its qualifier or its id"
			e.Candidates, e.Pass, e.Given = accountCandidates(matches), accountPicks(matches), query
			return e
		})
}

// accountLabel is one picker row, in the shape the list uses: the name bold, the id grey, then the
// state.
func accountLabel(t out.Theme, r account.Resolved, now time.Time) string {
	return t.Bold(r.Name) + " " + t.Grey(r.ID) + " " + r.State.Text(r.Expired, now)
}

// accountCandidates names each match the way a selector would: its qualifier, then its id, since
// two accounts may share a name and only the id always tells them apart.
func accountCandidates(accounts []account.Resolved) []string {
	names := make([]string, len(accounts))
	for i, r := range accounts {
		names[i] = r.Qualifier() + " — " + r.ID
	}
	return names
}

func accountPicks(accounts []account.Resolved) []string {
	picks := make([]string, len(accounts))
	for i, r := range accounts {
		picks[i] = r.ID
	}
	return picks
}
