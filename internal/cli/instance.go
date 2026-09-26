package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/schema"
)

// Where a setting's value came from, as `instance get` reports it.
const (
	fromInstance = "instance"
	fromConfig   = "config"
	fromDefault  = "default"
)

// instanceSetting is one key as `instance get` reports it: the value in effect, where it came from,
// and what `instance unset` would return it to.
type instanceSetting struct {
	Path    string `json:"path"`
	Value   any    `json:"value"`
	From    string `json:"from"`
	Default any    `json:"default,omitempty"`
}

type instanceEdit struct {
	File    string `json:"file"`
	Changed bool   `json:"changed"`
}

func (a *app) instanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "instance",
		Short: "Read and change the settings of the instance you are in, or the one -i names, and dump or read its game",
	}
	cmd.AddCommand(a.instanceGetCmd(), a.instanceSetCmd(), a.instanceUnsetCmd(), a.instanceEditCmd(), a.instanceDumpCmd(), a.instanceLogCmd())
	return cmd
}

func (a *app) instanceGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "get [path]",
		Annotations: reads(),
		Short:       "Print a setting in effect and the default behind it, or every setting",
		Args:        maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := a.openInstanceFile()
			if err != nil {
				return err
			}
			defaults, err := a.playDefaults()
			if err != nil {
				return err
			}
			if len(args) == 0 {
				all := f.settings()
				for _, key := range instance.LaunchKeys {
					field, err := configField("play." + key)
					if err != nil {
						return err
					}
					if v, ok := field.Get(defaults); ok {
						if _, set := all[key]; !set {
							all[key] = v
						}
					}
				}
				return a.printer.Emit(all, func(l *out.Lines) { writeValue(l.W, all) })
			}
			field, err := f.fields.Lookup(args[0])
			if err != nil {
				return err
			}
			got := instanceSetting{Path: field.Path}
			global := globalKey(field)
			if global != "" {
				field, err := configField(global)
				if err != nil {
					return err
				}
				got.Default, _ = field.Get(defaults)
			} else {
				got.Default, _ = field.Default()
			}
			if v, ok := field.Get(f.doc); ok {
				got.Value, got.From = v, fromInstance
			} else if got.Default != nil {
				got.Value, got.From = got.Default, fromConfig
				if global == "" {
					got.From = fromDefault
				}
			} else {
				return out.Errorf("path-not-set", "%s is not set", field.Path)
			}
			return a.printer.Emit(got, func(l *out.Lines) {
				writeValue(l.W, got.Value)
				l.Muted(settingOrigin(got, global))
			})
		},
	}
}

// settingOrigin says where the value `instance get` printed came from, and for one the instance
// sets itself, what unsetting it would fall back to.
func settingOrigin(s instanceSetting, global string) string {
	switch {
	case s.From == fromConfig:
		return "from " + global
	case s.From == fromDefault:
		return "the default"
	case s.Default != nil && global != "":
		return "set here; " + global + " is " + valueText(s.Default)
	case s.Default != nil:
		return "set here; the default is " + valueText(s.Default)
	}
	return "set here"
}

// valueText is a value inside a sentence: a string as it is, anything else as JSON.
func valueText(v any) string {
	if text, ok := v.(string); ok {
		return text
	}
	return settingText(v)
}

// globalKey is the config.json key behind a setting, or empty for a setting only an instance has.
func globalKey(field *schema.Field) string {
	if len(field.Keys) == 2 && slices.Contains(instance.LaunchKeys, field.Keys[1]) {
		return "play." + field.Keys[1]
	}
	return ""
}

func (a *app) instanceSetCmd() *cobra.Command {
	var literal bool
	cmd := &cobra.Command{
		Use:         "set <path> <value>",
		Annotations: acts(),
		Short:       "Set a setting in this instance, over the default",
		Args:        exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := a.openInstanceFile()
			if err != nil {
				return err
			}
			field, err := f.fields.Lookup(args[0])
			if err != nil {
				return err
			}
			from, _ := field.Get(f.doc)
			var to any
			if literal {
				to, err = decodeLiteral(field.Path, args[1])
			} else {
				to, err = field.Coerce(args[1], from)
			}
			if err != nil {
				return err
			}
			if globalKey(field) != "" {
				if err := config.CheckPlaySetting(field.Path, field.Keys[1], to); err != nil {
					return err
				}
			}
			if field.Path == "account" {
				if to, err = a.pinnedAccountID(to); err != nil {
					return err
				}
			}
			field.Put(f.doc, to)
			if err := f.save(); err != nil {
				return err
			}
			return a.emitSettingChange(settingChange{Path: field.Path, From: from, To: to})
		},
	}
	cmd.Flags().BoolVar(&literal, "literal", false, "parse the value as JSON, for a list")
	return cmd
}

// pinnedAccountID is the id of the account a pin names. The id is what is written, so the pin still
// holds after the account's player renames themselves.
func (a *app) pinnedAccountID(v any) (string, error) {
	name, ok := v.(string)
	if !ok {
		return "", out.Errorf("usage", "account takes the name or id of an account")
	}
	r, err := a.selectAccount(name)
	if err != nil {
		return "", err
	}
	return r.ID, nil
}

func (a *app) instanceUnsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "unset <path>",
		Annotations: acts(),
		Short:       "Remove a setting from this instance, back to the default",
		Args:        exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f, err := a.openInstanceFile()
			if err != nil {
				return err
			}
			field, err := f.fields.Lookup(args[0])
			if err != nil {
				return err
			}
			from, ok := field.Get(f.doc)
			if !ok {
				return a.printer.Emit(settingChange{Path: field.Path}, func(l *out.Lines) {
					l.Info(field.Path + " was not set")
				})
			}
			field.Remove(f.doc)
			if err := f.save(); err != nil {
				return err
			}
			return a.emitSettingChange(settingChange{Path: field.Path, From: from})
		},
	}
}

func (a *app) instanceEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:         "edit",
		Annotations: acts(),
		Short:       "Open this instance's instance.json in your editor",
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if a.printer.NoInput || !a.tty() {
				e := out.Errorf("usage", "instance edit opens an editor, and there is no terminal to open it on")
				e.Help = "`shulker instance set` changes one setting"
				return e
			}
			// The file is opened unchecked: one that no longer matches the schema is the file most in
			// need of an editor.
			dir, err := a.instanceDir()
			if err != nil {
				return err
			}
			path := instance.Path(dir)
			before, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if err := a.runEditor(path); err != nil {
				return err
			}
			if _, err := instance.Load(dir); err != nil {
				return err
			}
			after, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			res := instanceEdit{File: path, Changed: !bytes.Equal(before, after)}
			return a.printer.Emit(res, func(l *out.Lines) {
				if res.Changed {
					l.OK("Saved "+res.File, "")
				} else {
					l.Info(res.File + " is unchanged")
				}
			})
		},
	}
}

// runEditor opens path in the editor $VISUAL or $EDITOR names, the way git does, and waits for it.
// The variable can carry arguments of its own, like "code --wait".
func (a *app) runEditor(path string) error {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = "vi"
		if runtime.GOOS == "windows" {
			editor = "notepad"
		}
	}
	words := strings.Fields(editor)
	cmd := exec.Command(words[0], append(words[1:], path)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = a.stdin, a.printer.Stdout, a.printer.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return out.Errorf("editor-failed", "%s exited with status %d, so the edit may not have been saved", words[0], exit.ExitCode())
		}
		e := out.Errorf("editor-failed", "can't run %s", words[0]).WithCause("os", err)
		e.Help = "set $EDITOR to the editor you use"
		return e
	}
	return nil
}

func (a *app) emitSettingChange(change settingChange) error {
	return a.printer.Emit(change, func(l *out.Lines) {
		printSettingChange(l, change.Path, change.From, change.To)
	})
}

// instanceFile is an instance's instance.json opened for its settings: the document as written, and
// the schema a settings-relative path is looked up in.
type instanceFile struct {
	dir, path string
	doc       map[string]any
	fields    *schema.FieldSet
}

// openInstanceFile opens the instance file of the directory the command acts on: the one it runs
// in, -C, or the instance -i names. Any directory shulker syncs into has one, so another launcher's
// instance has settings here too.
func (a *app) openInstanceFile() (*instanceFile, error) {
	dir, err := a.instanceDir()
	if err != nil {
		return nil, err
	}
	if _, err := instance.Load(dir); err != nil {
		return nil, err
	}
	path := instance.Path(dir)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, schema.Invalid("instance-invalid", path, data, err)
	}
	s, err := schema.Fields(schema.Instance, instance.FileName, "settings")
	if err != nil {
		return nil, err
	}
	return &instanceFile{dir: dir, path: path, doc: doc, fields: s}, nil
}

// instanceDir is the directory the command acts on — the one it runs in, -C, or the instance -i
// names — once it is known to hold an instance file.
func (a *app) instanceDir() (string, error) {
	dir, err := a.scopeDir()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(instance.Path(dir)); errors.Is(err, fs.ErrNotExist) {
		e := out.Errorf("instance-not-found", "%s is not an instance", dir)
		e.Help = "run this in one, or name one with -i"
		return "", e
	} else if err != nil {
		return "", err
	}
	return dir, nil
}

func (i *instanceFile) settings() map[string]any {
	s, _ := i.doc["settings"].(map[string]any)
	if s == nil {
		return map[string]any{}
	}
	return maps.Clone(s)
}

func (i *instanceFile) save() error { return instance.SaveDocument(i.dir, i.doc) }

// playDefaults is config.json as a document, for the play.* defaults an instance inherits.
func (a *app) playDefaults() (map[string]any, error) {
	path, err := a.configFile()
	if err != nil {
		return nil, err
	}
	return config.LoadDocument(path)
}
