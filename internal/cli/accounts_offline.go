package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/mojang"
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
		Use:         "add <name>",
		Annotations: acts(),
		Short:       "Create an offline account",
		Args:        exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if !allowInvalid && !player.IsName(name) {
				e := out.Errorf("account-name-invalid", "%q is not a Minecraft username", name)
				e.Rows = []out.Detail{{Label: "Wants", Text: "3 to 16 letters, digits or underscores"}}
				e.Nudge = out.Nudge{Lead: "Use it anyway", Command: "shulker accounts add " + account.QuoteName(name) + " --allow-invalid-name"}
				return e
			}
			id := account.OfflineUUID(name)
			if uuid != "" {
				if !player.IsUUID(uuid) {
					return out.Errorf("usage", "--uuid takes a player uuid, dashed or not, and %q is neither", uuid)
				}
				id = mojang.Dashed(uuid)
			}
			accounts, _, err := a.accounts()
			if err != nil {
				return err
			}
			if !account.OwnsTheGame(accounts) {
				return unprovenOwnership("create an offline one", out.Nudge{Lead: "Sign in to Microsoft", Command: "shulker accounts login"})
			}
			path, store, err := a.accountStore()
			if err != nil {
				return err
			}
			if err := account.FreeToCreate(accounts, store, name, id, force); err != nil {
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
		Use:         "remove <name>",
		Annotations: acts(),
		Short:       "Delete an offline account",
		Args:        exactArgs(1),
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
			if !force && !account.OwnsTheGame(accounts) {
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

func unprovenOwnership(what string, nudge out.Nudge) error {
	e := out.Errorf("ownership-unproven", "shulker can see no account that owns Minecraft: Java Edition, so it won't %s", what)
	e.Nudge = nudge
	return e
}

// notOfflineAccount is what `accounts remove` says about an account it doesn't delete, naming the
// verb that does act on it.
func notOfflineAccount(r account.Resolved) error {
	if r.Source == account.SourceShulker {
		e := out.Errorf("usage", "%s is a Microsoft account, so it is signed out rather than deleted", r.Name)
		e.Help = fmt.Sprintf("`shulker accounts logout %s` does that", accountSelector(r))
		return e
	}
	return notOwnAccount(r, "remove")
}

// offlineAccountOf is an account shulker created as every command that names one sees it.
func offlineAccountOf(a account.Account) account.Resolved {
	return account.Resolved{ID: a.ID(), Name: a.Name(), Source: account.SourceOffline, Group: account.GroupOffline, State: a.State(), Account: a}
}
