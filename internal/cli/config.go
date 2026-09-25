package cli

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/schema"
)

const accountsDefault = "accounts.default"

type configChange struct {
	Path    string `json:"path"`
	From    any    `json:"from,omitempty"`
	To      any    `json:"to,omitempty"`
	Created string `json:"created,omitempty"`
}

func (a *app) configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Read and change shulker's own config.json",
	}
	cmd.AddCommand(a.configGetCmd(), a.configSetCmd(), a.configUnsetCmd())
	return cmd
}

func (a *app) configGetCmd() *cobra.Command {
	var reveal bool
	cmd := &cobra.Command{
		Use:         "get [key]",
		Annotations: reads(),
		Short:       "Print a key of config.json, or all of it",
		Args:        maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var key string
			if len(args) == 1 {
				key = args[0]
			}
			path, cfg, doc, err := a.openConfig(key)
			if err != nil {
				return err
			}
			field, err := configField(key)
			if err != nil {
				return err
			}
			resolved, err := a.resolvedPaths(path, cfg)
			if err != nil {
				return err
			}
			var value any
			switch {
			case key == "":
				all := maps.Clone(doc)
				delete(all, "$schema")
				for k, v := range resolved {
					all[k] = v
				}
				if !reveal {
					all = config.Redact(all, maskKey)
				}
				value = all
			case resolved[key] != "":
				value = resolved[key]
			default:
				v, ok := field.Get(doc)
				if !ok {
					if v, ok = field.Default(); !ok {
						return out.Errorf("path-not-set", "%s is not set", key)
					}
				}
				if config.IsSecret(key) && !reveal {
					if secret, isText := v.(string); isText {
						v = maskKey(secret)
					}
				}
				value = v
			}
			return a.printer.Emit(value, func(l *out.Lines) { writeValue(l.W, value) })
		},
	}
	cmd.Flags().BoolVar(&reveal, "reveal", false, "print curseforge.key in full")
	return cmd
}

func (a *app) configSetCmd() *cobra.Command {
	var force, literal bool
	cmd := &cobra.Command{
		Use:         "set <key> <value>",
		Annotations: acts(),
		Short:       "Set a key in config.json",
		Args:        exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, value := args[0], args[1]
			path, cfg, doc, err := a.openConfig(key)
			unreadable := err
			if code := out.CodeOf(err); code == "config-invalid" || code == "schema-newer" {
				cfg, doc = config.Config{}, map[string]any{}
			} else if err != nil {
				return err
			}
			if value == "" {
				e := out.Errorf("usage", "%s needs a value", key)
				e.Help = fmt.Sprintf("`shulker config unset %s` removes it", key)
				return e
			}
			field, err := configField(key)
			if err != nil {
				return err
			}
			from, _ := field.Get(doc)
			var to any
			if literal {
				to, err = decodeLiteral(key, value)
			} else {
				to, err = field.Coerce(value, from)
			}
			if err != nil {
				return err
			}
			if to, err = config.CheckValue(key, to, launcher.AccountStores()); err != nil {
				return err
			}
			field.Put(doc, to)
			if err := checkConfigDocument(field, doc, to); err != nil {
				return err
			}
			if key == accountsDefault && unreadable == nil {
				if err := a.checkAccountID(to.(string)); err != nil {
					return err
				}
			}
			change := configChange{Path: key, From: from, To: to}
			if key == "registry" {
				next := cfg
				next.Registry = value
				if change.Created, err = config.SwitchRegistry(path, cfg, next, force); err != nil {
					return err
				}
			}
			if unreadable != nil {
				kept, err := config.ReplaceDocument(path, doc)
				if err != nil {
					return err
				}
				a.warnReplaced(unreadable, kept)
			} else if err := config.SaveDocument(path, doc); err != nil {
				return err
			}
			return a.emitConfigChange(change)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "change the registry even if it leaves linked instances behind")
	cmd.Flags().BoolVar(&literal, "literal", false, "parse the value as JSON, for a list or an object")
	return cmd
}

func (a *app) configUnsetCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:         "unset <key>",
		Annotations: acts(),
		Short:       "Remove a key from config.json",
		Args:        exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			path, cfg, doc, err := a.openConfig(key)
			if err != nil {
				return err
			}
			field, err := configField(key)
			if err != nil {
				return err
			}
			from, ok := field.Get(doc)
			if !ok {
				return a.printer.Emit(configChange{Path: key}, func(l *out.Lines) {
					l.Info(key + " was not set")
				})
			}
			change := configChange{Path: key, From: from}
			if key == "registry" {
				next := cfg
				next.Registry = ""
				if change.Created, err = config.SwitchRegistry(path, cfg, next, force); err != nil {
					return err
				}
			}
			field.RemoveEmptied(doc)
			if err := config.SaveDocument(path, doc); err != nil {
				return err
			}
			return a.emitConfigChange(change)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "change the registry even if it leaves linked instances behind")
	return cmd
}

// resolvedPaths is what each path-valued key means on this machine: the value in config.json when
// there is one, else the default behind it. `config get` prints these rather than the raw key, so
// an unset root still names the directory it will use.
func (a *app) resolvedPaths(configPath string, cfg config.Config) (map[string]string, error) {
	r, err := a.rootsOf(configPath, cfg)
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"registry":  config.RegistryPath(configPath, cfg),
		"instances": r.Instances,
		"saves":     r.Saves,
		"store":     r.Store,
	}, nil
}

// openConfig reads config.json for a command about key. A config that can't be read still returns
// its path beside the error, so `config set` can replace it.
func (a *app) openConfig(key string) (string, config.Config, map[string]any, error) {
	if key != "" && !slices.Contains(config.Keys, key) {
		e := out.Errorf("path-invalid", "config.json has no %q", key)
		e.Candidates, e.Given = config.Keys, key
		return "", config.Config{}, nil, e
	}
	path, err := a.configFile()
	if err != nil {
		return "", config.Config{}, nil, err
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		return path, config.Config{}, nil, err
	}
	doc, err := config.LoadDocument(path)
	if err != nil {
		return path, config.Config{}, nil, err
	}
	return path, cfg, doc, nil
}
func (a *app) emitConfigChange(change configChange) error {
	if config.IsSecret(change.Path) {
		if s, ok := change.From.(string); ok {
			change.From = maskKey(s)
		}
		if s, ok := change.To.(string); ok {
			change.To = maskKey(s)
		}
	}
	return a.printer.Emit(change, func(l *out.Lines) {
		l.Items(out.Item{Kind: out.Change, Name: change.Path, From: settingText(change.From), To: settingText(change.To)})
		if change.Created != "" {
			l.OK("created "+change.Created, "")
		}
	})
}

func maskKey(key string) string {
	const dots = "••••"
	if len(key) <= 4 {
		return dots
	}
	return dots + key[len(key)-4:]
}

var configSchema = sync.OnceValues(func() (*schema.FieldSet, error) {
	return schema.Fields(schema.Config, config.FileName)
})

// configField is key's field in config.json's schema, or nil for no key.
func configField(key string) (*schema.Field, error) {
	if key == "" {
		return nil, nil
	}
	s, err := configSchema()
	if err != nil {
		return nil, err
	}
	return s.Lookup(key)
}

// checkAccountID refuses a default account shulker can't see, which every launch would then fail on.
func (a *app) checkAccountID(id string) error {
	accounts, _, err := a.accounts()
	if err != nil {
		return err
	}
	if _, ok := account.ByID(accounts, id); ok {
		return nil
	}
	e := out.Errorf("account-not-found", "no account has the id %s", id)
	e.Help = "`shulker accounts` lists them"
	return e
}

// checkConfigDocument refuses a value, typed with --literal, that config.json's schema doesn't allow
// at field, so the next run doesn't find the file broken.
func checkConfigDocument(field *schema.Field, doc map[string]any, v any) error {
	written := maps.Clone(doc)
	written["$schema"] = schema.URL(schema.Config)
	data, err := json.Marshal(written)
	if err != nil {
		return err
	}
	if schema.Validate(schema.Config, data) != nil {
		return out.Errorf("usage", "%s takes %s, not %s", field.Path, field.Kind(), settingText(v))
	}
	return nil
}
