package cli

import (
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/player"
)

func (a *app) accountsAddCmd() *cobra.Command {
	var (
		uuid         string
		allowInvalid bool
		force        bool
		use          bool
	)
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Create an offline account",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if !allowInvalid && !player.IsName(name) {
				e := out.Errorf("account-name-invalid", "%q is not a Minecraft username", name)
				e.Rows = []out.Detail{{Label: "Wants", Text: "3 to 16 letters, digits or underscores"}}
				e.Nudge = out.Nudge{Lead: "Use it anyway", Command: "shulker accounts add " + quoteName(name) + " --allow-invalid-name"}
				return e
			}
			id := account.OfflineUUID(name)
			if uuid != "" {
				if !player.IsUUID(uuid) {
					return out.Errorf("usage", "--uuid takes a player uuid, dashed or not, and %q is neither", uuid)
				}
				id = player.Dashed(uuid)
			}
			accounts, _, err := a.accounts()
			if err != nil {
				return err
			}
			if !ownsTheGame(accounts) {
				return unprovenOwnership("create an offline one", out.Nudge{Lead: "Sign in to Microsoft", Command: "shulker accounts login"})
			}
			path, store, err := a.accountStore()
			if err != nil {
				return err
			}
			if err := freeToCreate(accounts, store, name, id, force); err != nil {
				return err
			}
			created := account.NewOffline(name, id)
			store.Put(created)
			if err := account.Save(path, store); err != nil {
				return err
			}
			r := offlineAccountOf(created)
			if use {
				if _, err := a.changeDefault(r.ID); err != nil {
					return err
				}
			}
			row := accountRow{ID: r.ID, Name: r.Name, Source: r.Source, Group: r.Group, State: r.State, Default: use}
			return a.printer.Emit(row, func(l *out.Lines) {
				text := "created the offline account " + r.Name
				if use {
					text += ", now the default account"
				}
				l.OK(text, r.ID)
				if !use {
					l.Nudge("Make it the default account", "shulker accounts use "+accountSelector(r))
				}
			})
		},
	}
	cmd.Flags().StringVar(&uuid, "uuid", "", "play under this uuid instead of the one the name derives")
	cmd.Flags().BoolVar(&allowInvalid, "allow-invalid-name", false, "take a name no Minecraft account could have")
	cmd.Flags().BoolVar(&force, "force", false, "create it even though an account already answers to that name")
	cmd.Flags().BoolVar(&use, "use", false, "make it the default account straight away")
	return cmd
}

func (a *app) accountsRemoveCmd() *cobra.Command {
	var yes, force bool
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Delete an offline account",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := a.selectAccount(args[0])
			if err != nil {
				return err
			}
			if r.Source != account.SourceOffline {
				return notOfflineAccount(r)
			}
			accounts, cfg, err := a.accounts()
			if err != nil {
				return err
			}
			if !force && !ownsTheGame(accounts) {
				return unprovenOwnership("delete an offline one it couldn't create again",
					out.Nudge{Lead: "Remove it anyway", Command: "shulker accounts remove " + accountSelector(r) + " --force"})
			}
			if !yes {
				remove, err := a.confirm("Remove "+r.Name+"?", "--yes")
				if err != nil {
					return err
				}
				if !remove {
					return a.printer.Emit(nil, func(l *out.Lines) { l.Info(r.Name + " is still there") })
				}
			}
			path, store, err := a.accountStore()
			if err != nil {
				return err
			}
			store.Remove(r.ID)
			if err := account.Save(path, store); err != nil {
				return err
			}
			gone := rowFor(r, cfg)
			moved, err := a.reseatDefault(gone)
			if err != nil {
				return err
			}
			row := accountRow{ID: r.ID, Name: r.Name, Source: r.Source, Group: r.Group, State: r.State}
			return a.printer.Emit(row, func(l *out.Lines) {
				l.OK("removed "+r.Name, r.ID)
				switch {
				case moved != nil:
					l.Info(moved.Name + " is the default account now")
				case gone.Default:
					l.Info("no default account now; `shulker accounts use <name>` picks one")
				}
			})
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "remove it without being asked first")
	cmd.Flags().BoolVar(&force, "force", false, "remove it with no account in sight that could create it again")
	return cmd
}

// ownsTheGame reports whether shulker can see an account that owns Java Edition: the gate an
// offline account passes at creation, and again at deletion, since the gate would block creating
// it a second time. It is a statement of intent rather than a licence check, so it reads the Java
// profile an account already carries and asks nothing of anybody. A profile is the proof whether
// or not its session still works: an expired sign-in or borrowed token changes who can launch, not
// who owns the game.
func ownsTheGame(accounts []account.Resolved) bool {
	for _, r := range accounts {
		switch r.State {
		case account.Playable, account.SignInExpired, account.TokenExpired:
			return true
		}
	}
	return false
}

func unprovenOwnership(what string, nudge out.Nudge) error {
	e := out.Errorf("ownership-unproven", "shulker can see no account that owns Minecraft: Java Edition, so it won't %s", what)
	e.Nudge = nudge
	return e
}

// freeToCreate is what an offline account may not be: an id another account already has, which is
// what shulker's own file is keyed by and so is refused however hard a run insists, or — without
// --force — a name someone else already answers to.
func freeToCreate(accounts []account.Resolved, store account.Store, name, id string, force bool) error {
	if have, taken := playsUnder(accounts, store, id); taken {
		e := out.Errorf("account-exists", "%s already plays under %s, and two accounts can't share a uuid", have, id)
		e.Nudge = out.Nudge{Lead: "Give the new account its own uuid", Command: "shulker accounts add " + quoteName(name) + " --force --uuid <uuid>"}
		return e
	}
	if force {
		return nil
	}
	for _, r := range accounts {
		if !strings.EqualFold(r.Name, name) {
			continue
		}
		e := out.Errorf("account-exists", "%s is already %s", name, whereItCameFrom(r))
		e.Rows = []out.Detail{{Label: "Have", Text: r.Qualifier() + " — " + r.ID}}
		e.Nudge = out.Nudge{Lead: "Create a second account with that name", Command: "shulker accounts add " + quoteName(name) + " --force --uuid <uuid>"}
		return e
	}
	return nil
}

// playsUnder names whoever already has an id. Shulker's own file is searched beside the accounts
// the providers yield, because storing an account is keyed by id: one the configured providers
// don't read back would be replaced rather than added.
func playsUnder(accounts []account.Resolved, store account.Store, id string) (string, bool) {
	for _, r := range accounts {
		if account.SameID(r.ID, id) {
			return r.Name, true
		}
	}
	for _, have := range store.Accounts {
		if account.SameID(have.ID(), id) {
			return have.Name(), true
		}
	}
	return "", false
}

// whereItCameFrom names an account the way a clash has to explain it: what is already there is
// either a sign-in, an offline account or another launcher's.
func whereItCameFrom(r account.Resolved) string {
	switch r.Source {
	case account.SourceShulker:
		return "signed in"
	case account.SourceOffline:
		return "an offline account"
	}
	return "borrowed from " + r.Source
}

// notOfflineAccount is what `accounts remove` says about an account it doesn't delete, naming the
// verb that does act on it.
func notOfflineAccount(r account.Resolved) error {
	if r.Source == account.SourceShulker {
		return out.Errorf("usage", "%s is a Microsoft account, so it is signed out rather than deleted; `shulker accounts logout %s` does that", r.Name, accountSelector(r))
	}
	return notOwnAccount(r, "remove")
}

// offlineAccountOf is an account shulker created as every command that names one sees it.
func offlineAccountOf(a account.Account) account.Resolved {
	return account.Resolved{ID: a.ID(), Name: a.Name(), Source: account.SourceOffline, Group: account.GroupOffline, State: a.State(), Account: a}
}
