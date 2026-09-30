package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/schema"
)

type settingChange struct {
	Path string `json:"path"`
	From any    `json:"from,omitempty"`
	To   any    `json:"to,omitempty"`
}

func (a *app) setCmd() *cobra.Command {
	var literal bool
	cmd := &cobra.Command{
		Use:         "set <path> <value>",
		Annotations: acts(),
		Short:       "Set a field in shulker.json by its dotted path",
		Args:        exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, doc, s, err := a.openSettings()
			if err != nil {
				return err
			}
			field, err := s.Lookup(args[0])
			if err != nil {
				return err
			}
			from, _ := field.Get(doc)
			var to any
			if literal {
				to, err = decodeLiteral(field.Path, args[1])
			} else {
				to, err = field.Coerce(args[1], from)
			}
			if err != nil {
				return err
			}
			field.Put(doc, to)
			return a.saveSettings(p, doc, field, from)
		},
	}
	a.scopeFlags(cmd)
	cmd.Flags().BoolVar(&literal, "literal", false, "parse the value as JSON, for lists, objects, or a value kept as a string.")
	return cmd
}

func (a *app) unsetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:         "unset <path>",
		Annotations: acts(),
		Short:       "Remove a field from shulker.json by its dotted path",
		Args:        exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, doc, s, err := a.openSettings()
			if err != nil {
				return err
			}
			field, err := s.Lookup(args[0])
			if err != nil {
				return err
			}
			from, ok := field.Get(doc)
			if !ok {
				return a.printer.Emit(settingChange{Path: field.Path}, func(l *out.Lines) {
					l.Info(field.Path + " was not set.")
				})
			}
			field.Remove(doc)
			return a.saveSettings(p, doc, field, from)
		},
	}
	a.scopeFlags(cmd)
	return cmd
}

func (a *app) getCmd() *cobra.Command {
	var locked bool
	cmd := &cobra.Command{
		Use:         "get [path]",
		Annotations: reads(),
		Short:       "Print a field of shulker.json, or all of it",
		Args:        maximumArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			open := a.openSettings
			if locked {
				open = a.openLocked
			}
			_, doc, s, err := open()
			if err != nil {
				return err
			}
			var value any = doc
			if len(args) == 1 {
				field, err := s.Lookup(args[0])
				if err != nil {
					return err
				}
				var ok bool
				if value, ok = field.Get(doc); !ok {
					return out.Errorf("path-not-set", "%s is not set", field.Path)
				}
			}
			return a.printer.Emit(value, func(l *out.Lines) { writeValue(l.W, value) })
		},
	}
	a.scopeFlags(cmd)
	cmd.Flags().BoolVar(&locked, "locked", false, "read shulker.lock instead.")
	return cmd
}

func printSettingChange(l *out.Lines, path string, from, to any) {
	if from != nil && settingText(from) == settingText(to) {
		l.Info(fmt.Sprintf("%s is already %s.", path, settingText(to)))
		return
	}
	l.Items(out.Item{Kind: out.Change, Name: path, From: settingText(from), To: settingText(to)})
}

func settingText(v any) string {
	if v == nil {
		return "(unset)"
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Sprint(v)
	}
	return strings.TrimSuffix(b.String(), "\n")
}

func writeValue(w io.Writer, value any) {
	if text, ok := value.(string); ok {
		fmt.Fprintln(w, text)
		return
	}
	data, err := fsutil.MarshalJSON(value)
	if err != nil {
		data = []byte(settingText(value))
	}
	fmt.Fprintf(w, "%s\n", bytes.TrimRight(data, "\n"))
}

func (a *app) openSettings() (*project.Project, map[string]any, *schema.FieldSet, error) {
	p, err := a.openProject()
	if err != nil {
		return nil, nil, nil, err
	}
	data, err := os.ReadFile(p.ManifestPath())
	if err != nil {
		return nil, nil, nil, err
	}
	var doc map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, nil, nil, schema.Invalid("manifest-invalid", manifest.FileName, data, err)
	}
	s, err := schema.Fields(schema.Manifest, manifest.FileName)
	if err != nil {
		return nil, nil, nil, err
	}
	return p, doc, s, nil
}

func (a *app) openLocked() (*project.Project, map[string]any, *schema.FieldSet, error) {
	p, err := a.openProject()
	if err != nil {
		return nil, nil, nil, err
	}
	if err := p.RequireLock(); err != nil {
		return nil, nil, nil, err
	}
	data, err := p.Lock.Encode()
	if err != nil {
		return nil, nil, nil, err
	}
	var doc map[string]any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, nil, nil, err
	}
	s, err := schema.Fields(schema.Lock, lock.FileName)
	if err != nil {
		return nil, nil, nil, err
	}
	return p, doc, s, nil
}

func (a *app) saveSettings(p *project.Project, doc map[string]any, field *schema.Field, from any) error {
	if err := p.ReplaceManifest(doc); err != nil {
		return err
	}
	saved, err := p.Manifest.Encode()
	if err != nil {
		return err
	}
	var written map[string]any
	dec := json.NewDecoder(bytes.NewReader(saved))
	dec.UseNumber()
	if err := dec.Decode(&written); err != nil {
		return err
	}
	change := settingChange{Path: field.Path, From: from}
	change.To, _ = field.Get(written)
	a.printer.LockStale = p.IsLockStale()
	if p.Lock != nil {
		a.warnLockDifferences(p)
	}
	return a.printer.Emit(change, func(l *out.Lines) {
		printSettingChange(l, change.Path, change.From, change.To)
	})
}

func decodeLiteral(path, value string) (any, error) {
	dec := json.NewDecoder(strings.NewReader(value))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil || dec.More() {
		e := out.Errorf("usage", "--literal takes a JSON value for %s, got %q", path, value)
		if runtime.GOOS == "windows" && quotesStripped(value) {
			e.Help = strippedQuotesHelp(value)
		}
		return nil, e
	}
	return v, nil
}

// quotesStripped reports whether value reads as a JSON list or object whose double quotes a shell
// took out, as Windows PowerShell 5.1 and cmd do. cmd also passes single quotes through, so a
// value wrapped in them counts too.
func quotesStripped(value string) bool {
	value = unwrapSingle(strings.TrimSpace(value))
	return (strings.HasPrefix(value, "[") || strings.HasPrefix(value, "{")) && !strings.Contains(value, `"`)
}

// strippedQuotesHelp shows the value with its double quotes put back and escaped, in the form each
// Windows shell passes on intact, since shulker can't tell which shell ran it.
func strippedQuotesHelp(value string) string {
	fixed, ok := requote(value)
	if !ok {
		return `the shell dropped the double quotes; escape each one as \"`
	}
	escaped := strings.ReplaceAll(fixed, `"`, `\"`)
	return "the shell dropped the double quotes; escape each one: '" + escaped + "' in PowerShell 5.1, " + escaped + " in cmd"
}

// requote puts double quotes back around every bare word in a list or object whose quotes a shell
// dropped, leaving numbers, true, false and null bare. It reports false when the result still isn't
// JSON.
func requote(value string) (string, bool) {
	value = unwrapSingle(strings.TrimSpace(value))
	var b strings.Builder
	word := func(w string) {
		t := unwrapSingle(strings.TrimSpace(w))
		if t == "" || t == "true" || t == "false" || t == "null" || json.Valid([]byte(t)) && !strings.ContainsAny(t, "[]{}") {
			b.WriteString(t)
			return
		}
		q, _ := json.Marshal(t)
		b.Write(q)
	}
	start := 0
	for i, r := range value {
		if strings.ContainsRune("[]{},:", r) {
			word(value[start:i])
			b.WriteRune(r)
			start = i + 1
		}
	}
	word(value[start:])
	return b.String(), json.Valid([]byte(b.String()))
}

func unwrapSingle(s string) string {
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		return s[1 : len(s)-1]
	}
	return s
}
