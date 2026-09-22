package cli

import (
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/out"
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
	cmd.AddCommand(a.accountsLoginCmd(), a.accountsLogoutCmd(), a.accountsAddCmd(), a.accountsRemoveCmd(), a.accountsRefreshCmd(), a.accountsUseCmd(), a.accountsProvidersCmd())
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
			l.Info("no accounts yet; `shulker accounts login` signs in to Microsoft")
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
		{account.GroupBorrowed, "Borrowed"},
	} {
		if in := account.InGroup(accounts, g.group); len(in) > 0 {
			groups = append(groups, accountGroup{heading: g.heading, accounts: in})
		}
	}
	return groups
}

// writeAccounts prints a bold heading per kind and a list row per account: the name bold, the id
// grey and aligned as a version is, then the state. The default row takes the ok glyph in place of
// the grey dot, and carries no aside — the mark is the whole message.
func writeAccounts(l *out.Lines, groups []accountGroup, cfg config.Config) {
	now := time.Now()
	for i, g := range groups {
		if i > 0 {
			l.Blank()
		}
		l.Heading(g.heading)
		items := make([]out.Item, len(g.accounts))
		for j, r := range g.accounts {
			items[j] = out.Item{Kind: out.Note, Name: r.Name, Version: r.ID, Text: r.State.Text(r.Expired, now)}
			if isDefault(r, cfg) {
				items[j].Kind = out.Good
			}
		}
		l.Items(items...)
	}
}

// rowFor is one account as a command prints it, and as --json carries it.
func rowFor(r account.Resolved, cfg config.Config) accountRow {
	return accountRow{ID: r.ID, Name: r.Name, Source: r.Source, Group: r.Group, State: r.State, Default: isDefault(r, cfg)}
}

func isDefault(r account.Resolved, cfg config.Config) bool {
	return cfg.Accounts.Default != "" && account.SameID(r.ID, cfg.Accounts.Default)
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
	change := configChange{Path: accountsDefault, To: id}
	if from, ok := configLookup(doc, accountsDefault); ok {
		change.From = from
	}
	if id == "" {
		configRemove(doc, accountsDefault)
	} else {
		configPut(doc, accountsDefault, id)
	}
	return change, config.SaveDocument(path, doc)
}

// accountStore is shulker's own accounts.json: where it is, and what it holds. Only the accounts
// shulker signed in or created itself are in there; a borrowed one is read where it lives.
func (a *app) accountStore() (string, account.Store, error) {
	path, err := a.configFile()
	if err != nil {
		return "", account.Store{}, err
	}
	store, err := account.Load(account.Path(path))
	return account.Path(path), store, err
}

// accounts is every account the configured providers yield, with the config that named them.
func (a *app) accounts() ([]account.Resolved, config.Config, error) {
	path, err := a.configFile()
	if err != nil {
		return nil, config.Config{}, err
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		return nil, config.Config{}, err
	}
	store, err := account.Load(account.Path(path))
	if err != nil {
		return nil, config.Config{}, err
	}
	providers := cfg.Accounts.Providers
	if providers == nil {
		providers = account.DefaultProviders()
	}
	for _, p := range account.WithoutReader(providers) {
		a.printer.Warn("shulker can't read %s's accounts yet, so accounts.providers lists it for nothing", p)
	}
	borrowed, err := a.borrowedAccounts(providers)
	if err != nil {
		return nil, config.Config{}, err
	}
	return account.Resolve(providers, store, borrowed), cfg, nil
}
