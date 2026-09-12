package cli

import (
	"fmt"
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/out"
)

const curseForgeKey = "curseforge.key"

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
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(a.configGetCmd(), a.configSetCmd(), a.configUnsetCmd())
	return cmd
}

func (a *app) configGetCmd() *cobra.Command {
	var reveal bool
	cmd := &cobra.Command{
		Use:   "get [key]",
		Short: "Print a key of config.json, or all of it",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var key string
			if len(args) == 1 {
				key = args[0]
			}
			path, cfg, doc, err := a.openConfig(key)
			if err != nil {
				return err
			}
			var value any
			switch key {
			case "":
				all := maps.Clone(doc)
				all["registry"] = config.RegistryPath(path, cfg)
				if secret, ok := configLookup(doc, curseForgeKey); ok && !reveal {
					cf := maps.Clone(doc["curseforge"].(map[string]any))
					cf["key"] = maskKey(secret)
					all["curseforge"] = cf
				}
				value = all
			case "registry":
				value = config.RegistryPath(path, cfg)
			default:
				secret, ok := configLookup(doc, key)
				if !ok {
					return out.Errorf("path-not-set", "%s is not set", key)
				}
				if !reveal {
					secret = maskKey(secret)
				}
				value = secret
			}
			return a.printer.Emit(value, func(w io.Writer) { writeValue(w, value) })
		},
	}
	cmd.Flags().BoolVar(&reveal, "reveal", false, "print curseforge.key in full")
	return cmd
}

func (a *app) configSetCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a key in config.json",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, value := args[0], args[1]
			path, cfg, doc, err := a.openConfig(key)
			if err != nil {
				return err
			}
			if value == "" {
				return out.Errorf("usage", "%s needs a value; `shulker config unset %s` removes it", key, key)
			}
			change := configChange{Path: key, To: value}
			if from, ok := configLookup(doc, key); ok {
				change.From = from
			}
			if key == "registry" {
				next := cfg
				next.Registry = value
				if change.Created, err = a.switchRegistry(path, cfg, next, force); err != nil {
					return err
				}
			}
			configPut(doc, key, value)
			if err := config.SaveDocument(path, doc); err != nil {
				return err
			}
			return a.emitConfigChange(change)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "change the registry even if it leaves linked instances behind")
	return cmd
}

func (a *app) configUnsetCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "unset <key>",
		Short: "Remove a key from config.json",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			path, cfg, doc, err := a.openConfig(key)
			if err != nil {
				return err
			}
			from, ok := configLookup(doc, key)
			if !ok {
				return a.printer.Emit(configChange{Path: key}, func(w io.Writer) {
					fmt.Fprintf(w, "%s was not set.\n", key)
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
			configRemove(doc, key)
			if err := config.SaveDocument(path, doc); err != nil {
				return err
			}
			return a.emitConfigChange(change)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "change the registry even if it leaves linked instances behind")
	return cmd
}

func (a *app) openConfig(key string) (string, config.Config, map[string]any, error) {
	if key != "" && !slices.Contains(config.Keys, key) {
		return "", config.Config{}, nil, pathInvalid("config.json", key, config.Keys)
	}
	path, err := a.configFile()
	if err != nil {
		return "", config.Config{}, nil, err
	}
	cfg, err := config.LoadFile(path)
	if err != nil {
		return "", config.Config{}, nil, err
	}
	doc, err := config.LoadDocument(path)
	if err != nil {
		return "", config.Config{}, nil, err
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
	target, err := config.LoadLinks(to)
	if err != nil {
		return "", err
	}
	if !force {
		links, err := config.LoadLinks(from)
		if err != nil {
			return "", err
		}
		var left []string
		for _, l := range links {
			if _, ok := config.FindLink(target, l.Dir); !ok {
				left = append(left, l.Dir)
			}
		}
		if len(left) > 0 {
			entries := fmt.Sprintf("%d linked instances or synced directories", len(left))
			if len(left) == 1 {
				entries = "1 linked instance or synced directory"
			}
			e := out.Errorf("registry-has-links", "changing the registry leaves %s behind in %s; run again with --force to change it anyway", entries, from)
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
	return a.printer.Emit(change, func(w io.Writer) {
		fmt.Fprintf(w, "%s: %s -> %s\n", change.Path, settingText(change.From), settingText(change.To))
		if change.Created != "" {
			fmt.Fprintf(w, "created %s\n", change.Created)
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

func configLookup(doc map[string]any, key string) (string, bool) {
	m := doc
	if parent, name, nested := strings.Cut(key, "."); nested {
		m, _ = doc[parent].(map[string]any)
		key = name
	}
	s, ok := m[key].(string)
	return s, ok && s != ""
}

func configPut(doc map[string]any, key, value string) {
	parent, name, nested := strings.Cut(key, ".")
	if !nested {
		doc[key] = value
		return
	}
	m, ok := doc[parent].(map[string]any)
	if !ok {
		m = map[string]any{}
		doc[parent] = m
	}
	m[name] = value
}

func configRemove(doc map[string]any, key string) {
	parent, name, nested := strings.Cut(key, ".")
	if !nested {
		delete(doc, key)
		return
	}
	if m, ok := doc[parent].(map[string]any); ok {
		delete(m, name)
		if len(m) == 0 {
			delete(doc, parent)
		}
	}
}
