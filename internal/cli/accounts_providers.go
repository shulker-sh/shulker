package cli

import (
	"errors"
	"os"
	"slices"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/out"
)

func (a *app) accountsProvidersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "providers",
		Short: "List the launchers shulker reads accounts from",
		Args:  exactArgs(0),
		RunE:  func(cmd *cobra.Command, args []string) error { return a.listProviders() },
	}
	cmd.AddCommand(a.providersAddCmd(), a.providersRemoveCmd(), a.providersSetCmd())
	return cmd
}

func (a *app) listProviders() error {
	providers, err := a.providers()
	if err != nil {
		return err
	}
	return a.printer.Emit(providers, func(l *out.Lines) {
		items := make([]out.Item, len(providers))
		for i, p := range providers {
			items[i] = out.Item{Kind: out.Note, Name: p, Version: launcher.Title(p)}
			if !account.HasReader(p) {
				items[i].Text = "no reader yet"
			}
		}
		l.Items(items...)
	})
}

func (a *app) providersAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <launcher>",
		Short: "Read accounts from another launcher as well",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			from, err := a.providers()
			if err != nil {
				return err
			}
			if slices.Contains(from, args[0]) {
				return out.Errorf("usage", "%s already reads accounts from %s", accountsProviders, args[0])
			}
			return a.changeProviders(from, append(slices.Clone(from), args[0]))
		},
	}
}

func (a *app) providersRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <launcher>",
		Short: "Stop reading accounts from a launcher",
		Args:  exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			from, err := a.providers()
			if err != nil {
				return err
			}
			to := slices.DeleteFunc(slices.Clone(from), func(p string) bool { return p == args[0] })
			if len(to) == len(from) {
				e := out.Errorf("usage", "%s does not read accounts from %s", accountsProviders, args[0])
				e.Candidates, e.Given = from, args[0]
				return e
			}
			return a.changeProviders(from, to)
		},
	}
}

func (a *app) providersSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <launcher...>",
		Short: "Replace the list, in the order given",
		RunE: func(cmd *cobra.Command, args []string) error {
			from, err := a.providers()
			if err != nil {
				return err
			}
			return a.changeProviders(from, args)
		},
	}
}

// changeProviders writes the list and prints it before and after, in the shape `config set` gives
// the same key. A launcher newly in the list is checked for where it keeps its accounts, which is
// the one moment a player asked about it; the readers themselves stay quiet on every run after.
func (a *app) changeProviders(from, to []string) error {
	if err := checkProviders(to); err != nil {
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
	configPut(doc, accountsProviders, to)
	if err := config.SaveDocument(path, doc); err != nil {
		return err
	}
	a.warnMissingLaunchers(from, to)
	return a.emitConfigChange(configChange{Path: accountsProviders, From: from, To: to})
}

func (a *app) warnMissingLaunchers(from, to []string) {
	for _, p := range to {
		if p == account.SourceShulker || slices.Contains(from, p) {
			continue
		}
		dir, err := a.launcherDir(p)
		if err != nil || dir == "" {
			continue
		}
		if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
			a.printer.Warn("%s isn't at %s, so there are no accounts to read there yet", launcher.Title(p), dir)
		}
	}
}

// providers is the configured provider list, or the default when config.json doesn't name one.
func (a *app) providers() ([]string, error) {
	path, err := a.configFile()
	if err != nil {
		return nil, err
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		return nil, err
	}
	if cfg.Accounts.Providers == nil {
		return account.DefaultProviders(), nil
	}
	return cfg.Accounts.Providers, nil
}

// borrowedAccounts is what each configured launcher's reader takes from its own accounts file. A
// file that doesn't read warns and is skipped: shulker neither wrote it nor can repair it, so a
// corrupt one must not take the whole account list down with it.
func (a *app) borrowedAccounts(providers []string) (map[string][]account.Resolved, error) {
	var borrowed map[string][]account.Resolved
	now := time.Now()
	for _, p := range providers {
		if p == account.SourceShulker || !account.HasReader(p) {
			continue
		}
		dir, err := a.launcherDir(p)
		if err != nil {
			return nil, err
		}
		found, err := account.Read(p, dir, now)
		if err != nil {
			a.printer.Warn("%s", err)
			continue
		}
		if len(found) == 0 {
			continue
		}
		if borrowed == nil {
			borrowed = map[string][]account.Resolved{}
		}
		borrowed[p] = found
	}
	return borrowed, nil
}

// launcherDir is where a launcher keeps its own files: the directory a registered instance was
// linked against, since the player named it with `link --launcher-dir` and re-deriving would read
// a folder they don't use, and the launcher's default on this machine otherwise.
func (a *app) launcherDir(name string) (string, error) {
	instances, err := a.loadInstances()
	if err != nil {
		return "", err
	}
	for _, in := range instances {
		if in.Launcher == name && in.LauncherDir != "" {
			return in.LauncherDir, nil
		}
	}
	e := launcher.Find(name)
	if e == nil || e.DefaultDir == nil {
		return "", nil
	}
	// A default that can't be worked out on this machine is no directory at all, the way
	// scanLaunchers reads one: a launcher shulker can't place has nothing to read.
	dir, err := e.DefaultDir()
	if err != nil {
		return "", nil
	}
	return dir, nil
}
