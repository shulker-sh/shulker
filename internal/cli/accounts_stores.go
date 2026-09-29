package cli

import (
	"errors"
	"os"
	"slices"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/out"
)

func (a *app) accountsStoresCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:         "stores",
		Annotations: reads(),
		Short:       "List the launchers shulker reads accounts from",
		Args:        exactArgs(0),
		RunE:        func(cmd *cobra.Command, args []string) error { return a.listStores() },
	}
	cmd.AddCommand(a.storesAddCmd(), a.storesRemoveCmd(), a.storesSetCmd())
	return cmd
}

func (a *app) listStores() error {
	stores, err := a.stores()
	if err != nil {
		return err
	}
	return a.printer.Emit(stores, func(l *out.Lines) {
		items := make([]out.Item, len(stores))
		for i, name := range stores {
			items[i] = out.Item{Kind: out.Note, Name: name, Version: launcher.Title(name)}
			if name == account.SourceShulker {
				items[i] = out.Item{Kind: out.Note, Name: name, Aside: []string{"built in"}}
			}
		}
		l.Items(items...)
	})
}

func (a *app) storesAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "add <launcher>",
		Annotations: acts(),
		Short:       "Read accounts from another launcher as well",
		Args:        exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			from, err := a.stores()
			if err != nil {
				return err
			}
			if slices.Contains(from, args[0]) {
				return a.printer.Emit(configChange{Path: config.AccountsStoresKey, From: from, To: from}, func(l *out.Lines) {
					l.Info("Config key " + config.AccountsStoresKey + " already reads accounts from " + args[0] + ".")
				})
			}
			return a.changeStores(from, append(slices.Clone(from), args[0]))
		},
	}
}

func (a *app) storesRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "remove <launcher>",
		Annotations: acts(),
		Short:       "Stop reading accounts from a launcher",
		Args:        exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			from, err := a.stores()
			if err != nil {
				return err
			}
			if err := config.CheckStores(args, launcher.AccountStores()); err != nil {
				return err
			}
			if !slices.Contains(from, args[0]) {
				return a.printer.Emit(configChange{Path: config.AccountsStoresKey, From: from, To: from}, func(l *out.Lines) {
					l.Info("Config key " + config.AccountsStoresKey + " doesn't read accounts from " + args[0] + ".")
				})
			}
			to := slices.DeleteFunc(slices.Clone(from), func(p string) bool { return p == args[0] })
			return a.changeStores(from, to)
		},
	}
}

func (a *app) storesSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "set <launcher...>",
		Annotations: acts(),
		Short:       "Replace the list, in the order given",
		RunE: func(cmd *cobra.Command, args []string) error {
			from, err := a.stores()
			if err != nil {
				return err
			}
			return a.changeStores(from, args)
		},
	}
}

// changeStores writes the list and prints it before and after, in the shape `config set` gives
// the same key. A launcher newly in the list is checked for where it keeps its accounts, which is
// the one moment a player asked about it; the readers themselves stay quiet on every run after.
func (a *app) changeStores(from, to []string) error {
	if err := config.CheckStores(to, launcher.AccountStores()); err != nil {
		return err
	}
	path, err := a.configFile()
	if err != nil {
		return err
	}
	doc, err := config.LoadDocument(path)
	if err != nil {
		return err
	}
	field, err := configField(config.AccountsStoresKey)
	if err != nil {
		return err
	}
	field.Put(doc, to)
	if err := config.SaveDocument(path, doc); err != nil {
		return err
	}
	a.warnMissingLaunchers(from, to)
	return a.emitConfigChange(configChange{Path: config.AccountsStoresKey, From: from, To: to})
}

func (a *app) warnMissingLaunchers(from, to []string) {
	instances, err := a.loadInstances()
	if err != nil {
		return
	}
	for _, name := range to {
		e := launcher.Find(name)
		if e == nil || e.Accounts == nil || slices.Contains(from, name) {
			continue
		}
		dir := e.AccountsDir(instances)
		if dir == "" {
			continue
		}
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			a.printer.Warn("%s isn't at %s, so there are no accounts to read there yet.", e.Title, dir)
		}
	}
}

// stores is the configured store list, or the default when config.json doesn't name one.
func (a *app) stores() ([]string, error) {
	path, err := a.configFile()
	if err != nil {
		return nil, err
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		return nil, err
	}
	if cfg.Accounts.Stores == nil {
		return account.DefaultStores(), nil
	}
	return cfg.Accounts.Stores, nil
}
