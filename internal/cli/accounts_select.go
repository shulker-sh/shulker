package cli

import (
	"errors"
	"time"

	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/out"
)

// selectAccount resolves what an account argument names, asking on a terminal when the input
// can't tell several apart.
func (a *app) selectAccount(query string) (account.Resolved, error) {
	accounts, _, err := a.accounts()
	if err != nil {
		return account.Resolved{}, err
	}
	return account.Select(accounts, query, a.printer.Warn, a.accountPicker())
}

// accountPicker is the "Which account?" question on a terminal, and nil where there is none.
func (a *app) accountPicker() account.Picker {
	if !a.canPick() {
		return nil
	}
	return func(matches []account.Resolved) (account.Resolved, bool, error) {
		t, now := a.printer.ErrTheme, time.Now()
		choices := make([]out.Choice, len(matches))
		for i, r := range matches {
			choices[i] = out.Choice{Label: accountLabel(t, r, now), Value: r.ID}
		}
		chosen, err := a.questions().Pick("Which account?", choices, a.stdin)
		if errors.Is(err, out.ErrPickCancelled) {
			return account.Resolved{}, false, nil
		}
		if err != nil {
			return account.Resolved{}, false, err
		}
		for _, r := range matches {
			if r.ID == chosen {
				return r, true, nil
			}
		}
		return account.Resolved{}, false, nil
	}
}

// accountLabel is one picker row, in the shape the list uses: the name bold, the id grey, then the
// state.
func accountLabel(t out.Theme, r account.Resolved, now time.Time) string {
	return t.Bold(r.Name) + " " + t.Grey(r.ID) + " " + r.State.Text(r.Expired, now)
}
