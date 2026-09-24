package cli

import (
	"time"

	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/out"
)

// launchAccount is who a launch plays as: the account a selector names, the default account, or —
// with no default — the only account that could play, or the one a picker chooses. A choice made
// here becomes the default, which is what the second return says, so a launch asks at most once.
func (a *app) launchAccount(selector string) (account.Resolved, bool, error) {
	if selector != "" {
		r, err := a.selectAccount(selector)
		return r, false, err
	}
	accounts, cfg, err := a.accounts()
	if err != nil {
		return account.Resolved{}, false, err
	}
	if len(accounts) == 0 {
		e := out.Errorf("no-accounts", "shulker can see no accounts, so there is nothing to play with")
		e.Nudge = out.Nudge{Lead: "Sign in to Microsoft", Command: "shulker accounts login"}
		return account.Resolved{}, false, e
	}
	for _, r := range accounts {
		if isDefault(r, cfg) {
			return r, false, nil
		}
	}
	usable := account.Launchable(accounts)
	switch {
	case len(usable) == 0 && len(accounts) == 1:
		// One account and it can't play: its own state says what to do about that.
		return account.Resolved{}, false, accounts[0].State.Error(accounts[0].Name)
	case len(usable) == 0:
		return account.Resolved{}, false, noDefaultAccount(accounts)
	case len(usable) == 1:
		return usable[0], true, a.adoptDefault(usable[0])
	}
	r, err := pickOne(a, "Which account?", usable,
		func(r account.Resolved) string { return r.ID },
		func(r account.Resolved) string { return accountLabel(a.printer.ErrTheme, r, time.Now()) },
		func() error { return noDefaultAccount(usable) })
	if err != nil {
		return account.Resolved{}, false, err
	}
	return r, true, a.adoptDefault(r)
}

// adoptDefault makes the account a launch resolved the default one, so the question is asked once
// rather than before every game.
func (a *app) adoptDefault(r account.Resolved) error {
	_, err := a.changeDefault(r.ID)
	return err
}

// noDefaultAccount is what a launch says when there is no default account and nothing to ask on.
// The matches and the flag that names one are what the player needs either way, so escaping the
// picker lands here too.
func noDefaultAccount(accounts []account.Resolved) error {
	e := out.Errorf("usage", "no default account, and shulker has no terminal to ask on")
	e.Help = "name one with --account"
	e.Candidates, e.Pass, e.Flag = accountCandidates(accounts), accountPicks(accounts), "--account"
	return e
}
