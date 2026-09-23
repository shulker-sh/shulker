package cli

import (
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/account"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/schema"
)

const (
	curseForgeKey     = "curseforge.key"
	accountsProviders = "accounts.providers"
	accountsDefault   = "accounts.default"
	playSaveBackups   = "play.saveBackups"
	logKeepDays       = "log.keepDays"
)

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
				if cf, ok := doc["curseforge"].(map[string]any); ok && !reveal {
					if secret, ok := cf["key"].(string); ok {
						cf = maps.Clone(cf)
						cf["key"] = maskKey(secret)
						all["curseforge"] = cf
					}
				}
				value = all
			case resolved[key] != "":
				value = resolved[key]
			default:
				v, ok := field.get(doc)
				if !ok {
					if v, ok = field.schema["default"]; !ok {
						return out.Errorf("path-not-set", "%s is not set", key)
					}
				}
				if key == curseForgeKey && !reveal {
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
			from, _ := field.get(doc)
			var to any
			if literal {
				to, err = decodeLiteral(key, value)
			} else {
				to, err = field.coerce(value, from)
			}
			if err != nil {
				return err
			}
			if to, err = checkConfigValue(key, to); err != nil {
				return err
			}
			field.put(doc, to)
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
				if change.Created, err = a.switchRegistry(path, cfg, next, force); err != nil {
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
			from, ok := field.get(doc)
			if !ok {
				return a.printer.Emit(configChange{Path: key}, func(l *out.Lines) {
					l.Info(key + " was not set")
				})
			}
			change := configChange{Path: key, From: from}
			if key == "registry" {
				next := cfg
				next.Registry = ""
				if change.Created, err = a.switchRegistry(path, cfg, next, force); err != nil {
					return err
				}
			}
			field.removeEmptied(doc)
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

// switchRegistry checks the registry next resolves to, creating it when missing, and returns its
// path when it did. Without force it refuses when the current registry holds entries the new one
// lacks, because shulker would stop syncing them.
func (a *app) switchRegistry(configPath string, current, next config.Config, force bool) (string, error) {
	from := config.RegistryPath(configPath, current)
	to := config.RegistryPath(configPath, next)
	if filepath.Clean(from) == filepath.Clean(to) {
		return "", nil
	}
	dest, err := config.LoadInstances(to)
	if err != nil {
		return "", err
	}
	if !force {
		instances, err := config.LoadInstances(from)
		if err != nil {
			return "", err
		}
		var left []string
		for _, in := range instances {
			if _, ok := config.FindInstance(dest, in.Dir); !ok {
				left = append(left, in.Dir)
			}
		}
		if len(left) > 0 {
			entries := fmt.Sprintf("%d instances", len(left))
			if len(left) == 1 {
				entries = "1 instance"
			}
			e := out.Errorf("registry-has-instances", "changing the registry leaves %s behind in %s", entries, from)
			e.Help = "run again with --force to change it anyway"
			e.Items = left
			return "", e
		}
	}
	created, err := config.CreateRegistry(to)
	if err != nil || !created {
		return "", err
	}
	return to, nil
}

func (a *app) emitConfigChange(change configChange) error {
	if change.Path == curseForgeKey {
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

var configSchema = sync.OnceValues(func() (*settingsSchema, error) {
	return loadSchemaAt(schema.Config, config.FileName)
})

// configField is key's field in config.json's schema, or nil for no key.
func configField(key string) (*settingField, error) {
	if key == "" {
		return nil, nil
	}
	s, err := configSchema()
	if err != nil {
		return nil, err
	}
	return s.lookup(key)
}

// checkAccountID refuses a default account shulker can't see, which every launch would then fail on.
func (a *app) checkAccountID(id string) error {
	accounts, _, err := a.accounts()
	if err != nil {
		return err
	}
	if slices.ContainsFunc(accounts, func(r account.Resolved) bool { return r.ID == id }) {
		return nil
	}
	e := out.Errorf("account-not-found", "no account has the id %s", id)
	e.Help = "`shulker accounts` lists them"
	return e
}

// checkConfigDocument refuses a value, typed with --literal, that config.json's schema doesn't allow
// at field, so the next run doesn't find the file broken.
func checkConfigDocument(field *settingField, doc map[string]any, v any) error {
	written := maps.Clone(doc)
	written["$schema"] = schema.URL(schema.Config)
	data, err := json.Marshal(written)
	if err != nil {
		return err
	}
	if schema.Validate(schema.Config, data) != nil {
		return out.Errorf("usage", "%s takes %s, not %s", field.path, field.kind(), settingText(v))
	}
	return nil
}

// checkConfigValue rejects a value config.json can hold but shulker can't use, at the point it is
// typed rather than on the next run that reads it.
func checkConfigValue(key string, v any) (any, error) {
	switch key {
	case accountsProviders:
		names, err := providerList(v)
		if err != nil {
			return nil, err
		}
		return names, nil
	case playSaveBackups:
		return backupCount(key, v)
	case logKeepDays:
		return dayCount(key, v)
	}
	if name, ok := strings.CutPrefix(key, "play."); ok {
		if err := checkPlaySetting(key, name, v); err != nil {
			return nil, err
		}
	}
	return v, nil
}

// backupCount reads a count typed plainly or with --literal; 0 turns automatic backups off.
func backupCount(key string, v any) (any, error) {
	n, err := strconv.Atoi(fmt.Sprint(v))
	if err != nil || n < 0 {
		return nil, out.Errorf("usage", "%s takes a whole number of backups, 0 for none, not %s", key, settingText(v))
	}
	return n, nil
}

// dayCount reads a count of days typed plainly or with --literal. A log keeping no days would
// drop every earlier run on each write, so the least is 1.
func dayCount(key string, v any) (any, error) {
	n, err := strconv.Atoi(fmt.Sprint(v))
	if err != nil || n < 1 {
		return nil, out.Errorf("usage", "%s takes a whole number of days, at least 1, not %s", key, settingText(v))
	}
	return n, nil
}

// playHints say what each launch setting takes, for the error that refuses a value it can't.
var playHints = map[string]string{
	"memory":  `a heap size like "6G"`,
	"jvmArgs": `a list of JVM arguments; pass --literal '["-XX:+UseZGC"]'`,
	"java":    "an absolute path to a java binary or a Java home",
	"window":  `a window size like "1280x720"`,
	"wrapper": `a command as a list of words; pass --literal '["gamemoderun"]'`,
}

// checkPlaySetting holds a launch setting to the rules of the instance key of the same name, which
// is the one the schema writes down, so a value an instance file would refuse can't be a default
// either. path is the key as it was typed, for the message.
func checkPlaySetting(path, name string, v any) error {
	doc := map[string]any{"$schema": instance.SchemaURL, "settings": map[string]any{name: v}}
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	valid := schema.Validate(schema.Instance, data) == nil
	if java, ok := v.(string); ok && name == "java" && !filepath.IsAbs(java) {
		valid = false
	}
	if !valid {
		return out.Errorf("usage", "%s takes %s, not %s", path, playHints[name], settingText(v))
	}
	return nil
}

// providerList reads accounts.providers out of a JSON value.
func providerList(v any) ([]string, error) {
	items, ok := v.([]any)
	if !ok {
		return nil, out.Errorf("usage", "%s takes a JSON array of provider names", accountsProviders)
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		name, ok := item.(string)
		if !ok {
			return nil, out.Errorf("usage", "%s takes provider names, not %s", accountsProviders, settingText(item))
		}
		names = append(names, name)
	}
	if err := checkProviders(names); err != nil {
		return nil, err
	}
	return names, nil
}

// checkProviders refuses a provider list shulker can't act on, wherever it was typed: an empty one
// leaves no account anywhere, a repeat says nothing the first mention didn't, and a name no
// launcher answers to is a typo rather than a launcher shulker hasn't reached yet.
func checkProviders(names []string) error {
	if len(names) == 0 {
		e := out.Errorf("usage", "%s can't be empty", accountsProviders)
		e.Help = "unset it to go back to the default"
		e.Candidates = account.Providers()
		return e
	}
	for i, name := range names {
		if slices.Contains(names[:i], name) {
			return out.Errorf("usage", "%s names %s twice", accountsProviders, name)
		}
		if !slices.Contains(account.Providers(), name) {
			e := out.Errorf("usage", "shulker can't read accounts from %s", name)
			e.Candidates, e.Given = account.Providers(), name
			return e
		}
	}
	return nil
}
