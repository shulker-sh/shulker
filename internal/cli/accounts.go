package cli

import (
	"slices"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/play"
)

// accountRow is one line of `shulker accounts`, and what --json carries.
type accountRow struct {
	ID      string        `json:"id"`
	Name    string        `json:"name"`
	Source  string        `json:"source"`
	Group   account.Group `json:"group"`
	State   account.State `json:"state"`
	Default bool          `json:"default"`
}

func (a *app) accountsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:         "accounts",
		Annotations: reads(),
		Short:       "List the accounts shulker can play with",
		Args:        exactArgs(0),
		RunE:        func(cmd *cobra.Command, args []string) error { return a.listAccounts() },
	}
	cmd.AddCommand(a.accountsLoginCmd(), a.accountsLogoutCmd(), a.accountsAddCmd(), a.accountsRemoveCmd(), a.accountsRefreshCmd(), a.accountsUseCmd(), a.accountsStoresCmd())
	return cmd
}

func (a *app) listAccounts() error {
	accounts, cfg, err := a.accounts()
	if err != nil {
		return err
	}
	grouped := groupAccounts(accounts)
	rows := []accountRow{}
	for _, g := range grouped {
		for _, r := range g.accounts {
			rows = append(rows, rowFor(r, cfg))
		}
	}
	return a.printer.Emit(rows, func(l *out.Lines) {
		if len(rows) == 0 {
			l.Info("No accounts yet")
			l.Nudge("Sign in to Microsoft", "shulker accounts login")
			return
		}
		writeAccounts(l, grouped, cfg)
	})
}

// accountGroup is one block of the list: its heading and the accounts under it, in display order.
type accountGroup struct {
	heading  string
	accounts []account.Resolved
}

func groupAccounts(accounts []account.Resolved) []accountGroup {
	var groups []accountGroup
	for _, g := range []struct {
		group   account.Group
		heading string
	}{
		{account.GroupOwn, "Own"},
		{account.GroupOffline, "Offline"},
		{account.GroupLauncher, "Launcher"},
	} {
		if in := account.InGroup(accounts, g.group); len(in) > 0 {
			groups = append(groups, accountGroup{heading: g.heading, accounts: in})
		}
	}
	return groups
}

// writeAccounts prints one table for every account: the default's row takes the green ok glyph
// in the first column and carries no aside, since the mark is the whole message. The id column is
// UUID whatever kind of id the account has. The mark column shows only when an account is the
// default, and the Launcher column only when an account comes from outside shulker.
func writeAccounts(l *out.Lines, groups []accountGroup, cfg config.Config) {
	t := l.T
	now := time.Now()
	var accounts []account.Resolved
	for _, g := range groups {
		accounts = append(accounts, g.accounts...)
	}
	marks := slices.ContainsFunc(accounts, func(r account.Resolved) bool { return r.IsDefault(cfg.Accounts.Default) })
	launchers := slices.ContainsFunc(accounts, func(r account.Resolved) bool { return accountLauncher(r) != launcher.Title(account.SourceShulker) })
	headers := []string{"Account", "UUID"}
	styles := []lipgloss.Style{t.StyleBold(), t.StyleGrey()}
	if marks {
		headers = append([]string{""}, headers...)
		styles = append([]lipgloss.Style{t.StyleGreen().Inherit(t.StyleBold())}, styles...)
	}
	if launchers {
		headers = append(headers, "Launcher")
		styles = append(styles, t.StyleGrey())
	}
	headers = append(headers, "State")
	styles = append(styles, t.Style())
	var rows [][]string
	for _, r := range accounts {
		var row []string
		if marks {
			mark := ""
			if r.IsDefault(cfg.Accounts.Default) {
				mark = t.GlyphOK()
			}
			row = append(row, mark)
		}
		row = append(row, r.Name, r.ID)
		if launchers {
			row = append(row, accountLauncher(r))
		}
		rows = append(rows, append(row, r.State.Text(r.Expired, now)))
	}
	l.Table(headers, rows, out.Columns(styles...))
}

// accountLauncher names the launcher an account is read from; shulker's own and offline accounts
// both live in shulker.
func accountLauncher(r account.Resolved) string {
	if r.Source == account.SourceOffline {
		return launcher.Title(account.SourceShulker)
	}
	return launcher.Title(r.Source)
}

// rowFor is one account as a command prints it, and as --json carries it.
func rowFor(r account.Resolved, cfg config.Config) accountRow {
	return accountRow{ID: r.ID, Name: r.Name, Source: r.Source, Group: r.Group, State: r.State, Default: r.IsDefault(cfg.Accounts.Default)}
}

func (a *app) accountsUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "use <name>",
		Annotations: acts(),
		Short:       "Switch the default account",
		Args:        exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := a.selectAccount(args[0])
			if err != nil {
				return err
			}
			if r.State == account.NoProfile {
				return account.NoProfile.Error(r.Name)
			}
			change, err := a.changeDefault(r.ID)
			if err != nil {
				return err
			}
			return a.printer.Emit(change, func(l *out.Lines) {
				l.OK(r.Name+" is now the default account", r.ID)
			})
		},
	}
}

// changeDefault writes accounts.default, or takes the key out when id is empty, and answers with
// what changed so the command can print it.
func (a *app) changeDefault(id string) (configChange, error) {
	path, err := a.configFile()
	if err != nil {
		return configChange{}, err
	}
	doc, err := config.LoadDocument(path)
	if err != nil {
		return configChange{}, err
	}
	field, err := configField(accountsDefault)
	if err != nil {
		return configChange{}, err
	}
	change := configChange{Path: accountsDefault, To: id}
	change.From, _ = field.Get(doc)
	if id == "" {
		field.RemoveEmptied(doc)
	} else {
		field.Put(doc, id)
	}
	return change, config.SaveDocument(path, doc)
}

// accountStore is shulker's own accounts.json: where it is, and what it holds. Only the accounts
// shulker signed in or created itself are in there; one from a launcher is read where it lives.
func (a *app) accountStore() (string, account.Store, error) {
	path, err := a.configFile()
	if err != nil {
		return "", account.Store{}, err
	}
	store, err := account.Load(account.Path(path))
	return account.Path(path), store, err
}

// accounts is every account the configured stores yield, with the config that named them.
func (a *app) accounts() ([]account.Resolved, config.Config, error) {
	pe, err := a.playEnv()
	if err != nil {
		return nil, config.Config{}, err
	}
	return play.Accounts(pe)
}
