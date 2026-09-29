package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/out"
)

func (a *app) accountsLoginCmd() *cobra.Command {
	var use bool
	cmd := &cobra.Command{
		Use:         "login",
		Annotations: acts(),
		Short:       "Sign in to a Microsoft account",
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			d, err := a.deps()
			if err != nil {
				return err
			}
			path, store, err := a.accountStore()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			device, err := d.signin.Start(ctx)
			if err != nil {
				return err
			}
			a.showDeviceCode(device)
			a.printer.Working("waiting for the sign-in to finish")
			tokens, err := d.signin.Wait(ctx, device)
			if err != nil {
				return err
			}
			signed, err := d.signin.Complete(ctx, tokens)
			if err != nil {
				return err
			}
			store.Put(signed)
			if err := account.Save(path, store); err != nil {
				return err
			}
			r := signed.Resolved()
			_, cfg, err := a.accounts()
			if err != nil {
				return err
			}
			used := r.IsDefault(cfg.Accounts.Default)
			switch {
			case r.State == account.NoProfile:
				a.printer.Warn("%s owns no Java profile, so it can't launch or be the default account; Minecraft: Java Edition is at minecraft.net.", r.Name)
			case (use || cfg.Accounts.Default == "") && !used:
				if _, err := a.changeDefault(r.ID); err != nil {
					return err
				}
				used = true
			}
			row := accountRow{ID: r.ID, Name: r.Name, Source: r.Source, Group: r.Group, State: r.State, Default: used}
			return a.printer.Emit(row, func(l *out.Lines) {
				text := "Signed in as " + r.Name
				if used {
					text += ", now the default account"
				}
				l.OK(text, r.ID)
				if !used && r.State != account.NoProfile {
					l.Nudge("Make it the default account", "shulker accounts use "+accountSelector(r))
				}
			})
		},
	}
	cmd.Flags().BoolVar(&use, "use", false, "make it the default account straight away")
	return cmd
}

// showDeviceCode is the one thing a sign-in asks of the player: the page to open, and the code to
// type there on its own line. It goes to stderr, where every prompt goes, so a `--json` run still
// shows it.
func (a *app) showDeviceCode(d account.Device) {
	t, l := a.printer.ErrTheme, a.printer.Err()
	l.Blank()
	l.Plain(t.Grey("Sign in at ") + t.LinkURL(t.Command(d.URL), d.URL) + t.Grey(" with this code:"))
	l.Plain("  " + t.Bold(d.UserCode))
	l.Blank()
}

func (a *app) accountsLogoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:         "logout [name]",
		Annotations: acts(),
		Short:       "Sign out a Microsoft account",
		Args:        maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := a.selectOwnAccount(args, "sign out")
			if err != nil {
				return err
			}
			signOut, err := a.confirm("Sign " + r.Name + " out?")
			if err != nil {
				return err
			}
			if !signOut {
				return a.printer.Emit(nil, func(l *out.Lines) { l.Info(r.Name + " is still signed in") })
			}
			path, store, err := a.accountStore()
			if err != nil {
				return err
			}
			store.Remove(r.ID)
			if err := account.Save(path, store); err != nil {
				return err
			}
			moved, err := a.reseatDefault(r)
			if err != nil {
				return err
			}
			row := accountRow{ID: r.ID, Name: r.Name, Source: r.Source, Group: r.Group, State: r.State}
			return a.printer.Emit(row, func(l *out.Lines) {
				l.OK("Signed out "+r.Name, r.ID)
				switch {
				case moved != nil:
					l.Info(moved.Name + " is the default account now")
				case r.Default:
					l.Info("No default account")
					l.Nudge("Pick one", "shulker accounts use <name>")
				}
			})
		},
	}
	a.yesFlag(cmd, "sign out without being asked first")
	return cmd
}

func (a *app) accountsRefreshCmd() *cobra.Command {
	var missingProfile bool
	cmd := &cobra.Command{
		Use:         "refresh [name...]",
		Annotations: acts(),
		Short:       "Renew the accounts shulker signed in",
		RunE: func(cmd *cobra.Command, args []string) error {
			d, err := a.deps()
			if err != nil {
				return err
			}
			chosen, err := a.accountsToRenew(args, missingProfile)
			if err != nil {
				return err
			}
			path, store, err := a.accountStore()
			if err != nil {
				return err
			}
			rows, failures := []accountRow{}, []error{}
			for _, r := range chosen {
				a.printer.Working("renewing %s", r.Name)
				renewed, err := d.signin.Renew(cmd.Context(), r.Account)
				if err != nil {
					failures = append(failures, err)
					continue
				}
				store.Put(renewed)
				got := renewed.Resolved()
				rows = append(rows, accountRow{ID: got.ID, Name: got.Name, Source: got.Source, Group: got.Group, State: got.State})
			}
			// A run that renewed nothing fails with the first reason, rather than warning about
			// it and calling that a result.
			if len(rows) == 0 && len(failures) > 0 {
				return failures[0]
			}
			for _, err := range failures {
				a.printer.Warn("%s", renewFailed(err))
			}
			if err := account.Save(path, store); err != nil {
				return err
			}
			return a.printer.Emit(rows, func(l *out.Lines) {
				if len(rows) == 0 {
					l.Info("No account of shulker's own to renew")
					return
				}
				aside := ""
				if len(failures) > 0 {
					aside = fmt.Sprintf("%d couldn't be renewed", len(failures))
				}
				l.OK(renewedText(rows), aside)
			})
		},
	}
	cmd.Flags().BoolVar(&missingProfile, "missing-profile", false, "only the accounts that own no Java profile")
	return cmd
}

// accountsToRenew is which accounts a refresh acts on: shulker's own, the ones named, or the ones
// with no Java profile — and names and the filter together mean both at once.
func (a *app) accountsToRenew(names []string, missingProfile bool) ([]account.Resolved, error) {
	var chosen []account.Resolved
	if len(names) == 0 {
		accounts, _, err := a.accounts()
		if err != nil {
			return nil, err
		}
		chosen = account.Own(accounts)
	}
	for _, name := range names {
		r, err := a.selectAccount(name)
		if err != nil {
			return nil, err
		}
		if !r.IsOwn() {
			return nil, notOwnAccount(r, "renew")
		}
		chosen = append(chosen, r)
	}
	if missingProfile {
		chosen = account.WithoutProfile(chosen)
	}
	return chosen, nil
}

// selectOwnAccount is the account a command that writes accounts.json acts on. With no name it is
// the default account, which is the one every other command falls back to.
func (a *app) selectOwnAccount(args []string, verb string) (accountRow, error) {
	if len(args) == 1 {
		r, err := a.selectAccount(args[0])
		if err != nil {
			return accountRow{}, err
		}
		_, cfg, err := a.accounts()
		if err != nil {
			return accountRow{}, err
		}
		return rowFor(r, cfg), checkOwn(r, verb)
	}
	accounts, cfg, err := a.accounts()
	if err != nil {
		return accountRow{}, err
	}
	for _, r := range accounts {
		if r.IsDefault(cfg.Accounts.Default) {
			return rowFor(r, cfg), checkOwn(r, verb)
		}
	}
	e := out.Errorf("account-not-found", "no default account to %s", verb)
	e.Help = "name one"
	e.Candidates, e.Pass = account.Candidates(accounts), account.Picks(accounts)
	if len(accounts) == 0 {
		e = out.Errorf("no-accounts", "shulker has signed no account in, so there is nothing to %s", verb)
		e.Nudge = out.Nudge{Lead: "Sign in to Microsoft", Command: "shulker accounts login"}
	}
	return accountRow{}, e
}

func checkOwn(r account.Resolved, verb string) error {
	if r.IsOwn() {
		return nil
	}
	return notOwnAccount(r, verb)
}

// notOwnAccount is what a command that only acts on shulker's own accounts says about the others,
// naming the verb that does work on them.
func notOwnAccount(r account.Resolved, verb string) error {
	if r.Source == account.SourceOffline {
		e := out.Errorf("offline-account", "%s is an offline account, so there is no sign-in to %s", r.Name, verb)
		e.Help = fmt.Sprintf("`shulker accounts remove %s` deletes it", accountSelector(r))
		return e
	}
	return out.Errorf("launcher-account", "%s belongs to %s, so only %s can %s it", r.Name, r.Source, r.Source, verb)
}

// reseatDefault keeps accounts.default pointing at an account that is still there, handing it to
// the successor when there is one and clearing it otherwise.
func (a *app) reseatDefault(gone accountRow) (*account.Resolved, error) {
	if !gone.Default {
		return nil, nil
	}
	accounts, _, err := a.accounts()
	if err != nil {
		return nil, err
	}
	heir, ok := account.Successor(accounts)
	if !ok {
		_, err := a.changeDefault("")
		return nil, err
	}
	if _, err := a.changeDefault(heir.ID); err != nil {
		return nil, err
	}
	return &heir, nil
}

// accountSelector names an account the way it has to be typed back.
func accountSelector(r account.Resolved) string { return account.QuoteName(r.Name) }

// renewFailed is one account's failure as a warning, with the line that fixes it, since a refresh
// of several accounts carries on past the ones it can't renew.
func renewFailed(err error) string {
	e := out.AsError(err)
	if len(e.Rows) > 0 && e.Rows[0].IsCommand {
		return e.Message + "; run `" + e.Rows[0].Text + "`."
	}
	return e.Message
}

func renewedText(rows []accountRow) string {
	if len(rows) == 1 {
		return "Renewed " + rows[0].Name + "'s sign-in"
	}
	return fmt.Sprintf("Renewed %d sign-ins", len(rows))
}
