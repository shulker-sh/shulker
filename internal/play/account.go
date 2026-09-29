package play

import (
	"context"
	"time"

	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/out"
)

// Accounts is every account the configured stores yield, with the config that named them: shulker's
// own accounts.json, and what each launcher's reader takes from where that launcher keeps them.
func Accounts(e *Env) ([]account.Resolved, config.Config, error) {
	cfg, err := config.LoadFile(e.Config)
	if err != nil {
		return nil, config.Config{}, err
	}
	store, err := account.Load(account.Path(e.Config))
	if err != nil {
		return nil, config.Config{}, err
	}
	stores := cfg.Accounts.Stores
	if stores == nil {
		stores = account.DefaultStores()
	}
	fromLaunchers, err := launcherAccounts(e, stores)
	if err != nil {
		return nil, config.Config{}, err
	}
	return account.Resolve(stores, store, fromLaunchers), cfg, nil
}

func launcherAccounts(e *Env, stores []string) (map[string][]account.Resolved, error) {
	var fromLaunchers map[string][]account.Resolved
	instances, err := config.LoadInstances(e.Registry)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for _, name := range stores {
		l := launcher.Find(name)
		if l == nil || l.Accounts == nil {
			continue
		}
		found, errs := l.ReadAccounts(l.AccountsDir(instances), now)
		for _, err := range errs {
			e.Warn("%s", err)
		}
		if len(found) == 0 {
			continue
		}
		if fromLaunchers == nil {
			fromLaunchers = map[string][]account.Resolved{}
		}
		fromLaunchers[name] = found
	}
	return fromLaunchers, nil
}

// Account is who a launch plays as: the account the selector names, else the one the instance is
// pinned to, else the default account, else the only account that could play or the one AskAccount
// chooses. A choice made here should become the default, which is what adopt says, so a launch asks
// at most once; the caller writes it.
func Account(e *Env, plan *Plan, selector string) (who account.Resolved, adopt bool, err error) {
	accounts, cfg, err := Accounts(e)
	if err != nil {
		return account.Resolved{}, false, err
	}
	if selector == "" && plan.Settings.Account != "" {
		if selector, err = pinned(accounts, plan.Settings.Account); err != nil {
			return account.Resolved{}, false, err
		}
	}
	if selector != "" {
		r, err := account.Select(accounts, selector, e.Warn, e.AskAccount)
		return r, false, err
	}
	if len(accounts) == 0 {
		return account.Resolved{}, false, account.NoAccounts()
	}
	for _, r := range accounts {
		if r.IsDefault(cfg.Accounts.Default) {
			return r, false, nil
		}
	}
	usable := account.Launchable(accounts)
	switch {
	case len(usable) == 0 && len(accounts) == 1:
		// One account and it can't play: its own state says what to do about that.
		return account.Resolved{}, false, accounts[0].State.Error(accounts[0].Name)
	case len(usable) == 0:
		return account.Resolved{}, false, noDefault(accounts)
	case len(usable) == 1:
		return usable[0], true, nil
	case e.AskAccount == nil:
		return account.Resolved{}, false, noDefault(usable)
	}
	r, ok, err := e.AskAccount(usable)
	if err != nil {
		return account.Resolved{}, false, err
	}
	if !ok {
		return account.Resolved{}, false, noDefault(usable)
	}
	return r, true, nil
}

// pinned is the selector for the account an instance is pinned to. A pin whose account has gone
// fails the launch rather than playing as someone else: the pin is there because this instance is
// meant to be played as that account.
func pinned(accounts []account.Resolved, id string) (string, error) {
	if _, ok := account.ByID(accounts, id); ok {
		return id, nil
	}
	e := out.Errorf("account-not-found", "this instance is pinned to account %s, which shulker can no longer see", id)
	e.Candidates, e.Pass = account.Candidates(accounts), account.Picks(accounts)
	e.Nudge = out.Nudge{Lead: "Play it as the default account instead with", Command: "shulker instance unset account"}
	return "", e
}

// noDefault is what a launch says when there is no default account and nothing to ask on. The
// matches and the flag that names one are what the player needs either way, so escaping the
// picker lands here too.
func noDefault(accounts []account.Resolved) error {
	e := out.Errorf("usage", "no default account, and shulker has no terminal to ask on")
	e.Help = "name one with --account"
	e.Candidates, e.Pass, e.Flag = account.Candidates(accounts), account.Picks(accounts), "--account"
	return e
}

// Session is the account a launch plays on, renewed and saved when its token was stale, with the
// warning a session that online servers may reject carries.
func Session(ctx context.Context, e *Env, r account.Resolved) (account.Account, error) {
	signed, renewed, warning, err := e.SignIn.Session(ctx, r, time.Now())
	if err != nil {
		return account.Account{}, err
	}
	switch warning {
	case account.WarnTokenExpired:
		e.Warn("%s's session token has run out and only %s can renew it; online servers and Realms will reject this session.", r.Name, launcher.Title(r.Source))
	case account.WarnOffline:
		e.Warn("shulker couldn't reach Microsoft, so %s plays on the session it already had; online servers and Realms will reject it.", r.Name)
	}
	if !renewed {
		return signed, nil
	}
	path := account.Path(e.Config)
	store, err := account.Load(path)
	if err != nil {
		return account.Account{}, err
	}
	store.Put(signed)
	if err := account.Save(path, store); err != nil {
		return account.Account{}, err
	}
	return signed, nil
}
