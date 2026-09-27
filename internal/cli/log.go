package cli

import (
	"cmp"
	"fmt"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"shulker.sh/shulker/internal/auditlog"
	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider/curseforge"
)

const defaultLogWindow = "24h"

var logLevels = []string{auditlog.LevelInfo, auditlog.LevelWarn, auditlog.LevelError}

type logReport struct {
	Version  string            `json:"version"`
	Platform string            `json:"platform"`
	Since    string            `json:"since"`
	From     string            `json:"from"`
	KeepDays int               `json:"keepDays"`
	Filter   map[string]string `json:"filter"`
	Read     int               `json:"read"`
	Matched  int               `json:"matched"`
	Redacted bool              `json:"redacted"`
	Entries  []auditlog.Entry  `json:"entries"`
	// labels names each entry's instance as the instances table does, or is empty for an entry
	// whose instance is no longer registered.
	labels []string
}

type logFlags struct {
	since, group, cmd, code, level string
	unredacted                     bool
}

func (a *app) logCmd() *cobra.Command {
	var f logFlags
	cmd := &cobra.Command{
		Use:         "log",
		Annotations: map[string]string{logMode: logNever},
		Short:       "Show what shulker did, from its log",
		Args:        noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			now := time.Now()
			from, err := auditlog.ParseSince(f.since, now)
			if err != nil {
				e := out.Errorf("usage", "--since is neither a duration nor a date")
				e.Rows = []out.Detail{{Label: "Since", Text: f.since}}
				e.Help = "pass a duration like 24h or 7d, or a date like 2026-09-01"
				return e
			}
			if f.level != "" && !slices.Contains(logLevels, f.level) {
				return out.Errorf("usage", "--level is one of %s, not %q", strings.Join(logLevels, ", "), f.level)
			}
			filter := auditlog.Filter{Since: from, Group: f.group, Cmd: f.cmd, Code: f.code, Level: f.level}
			if a.instance != "" {
				found, err := a.selectInstances(a.instance, instanceSelection{})
				if err != nil {
					return err
				}
				in := found[0]
				filter.Instance = slices.DeleteFunc([]string{in.ID, in.Name, in.Dir}, func(s string) bool { return s == "" })
			}
			entries, err := auditlog.Read(a.log.Path)
			if err != nil {
				a.printer.Warn("can't read shulker's log at %s, so there is nothing to show: %v", a.log.Path, err)
				entries = nil
			}
			keepDays := configuredKeepDays()
			r := logReport{
				Version:  a.build().Version,
				Platform: runtime.GOOS + "/" + runtime.GOARCH,
				Since:    f.since,
				From:     from.UTC().Format(time.RFC3339),
				KeepDays: keepDays,
				Filter:   f.described(a.instance),
				Read:     len(entries),
				Entries:  []auditlog.Entry{},
			}
			for _, e := range entries {
				if filter.Match(e) {
					r.Entries = append(r.Entries, e)
				}
			}
			r.Matched = len(r.Entries)
			r.labels = a.instanceLabels(r.Entries)
			if !f.unredacted {
				redact(&r, logRedaction())
			}
			isWidest := !from.After(now.AddDate(0, 0, -keepDays))
			return a.printer.Emit(r, func(l *out.Lines) { printLog(l, r, isWidest) })
		},
	}
	cmd.Flags().StringVar(&f.since, "since", defaultLogWindow, "entries from this long ago (24h, 7d) or this date (2026-09-01) on")
	var groups []string
	for _, g := range helpGroups {
		groups = append(groups, g.ID)
	}
	cmd.Flags().StringVar(&f.group, "group", "", "only commands in this help group: "+strings.Join(groups, ", "))
	cmd.Flags().StringVar(&f.cmd, "cmd", "", "only this command and the ones under it, like sync or \"hook wrap\"")
	cmd.Flags().StringVar(&f.code, "code", "", "only entries with this error code")
	cmd.Flags().StringVar(&f.level, "level", "", "only entries at this level: info, warn, error")
	cmd.Flags().BoolVar(&f.unredacted, "unredacted", false, "print entries as stored, with the credentials, keys and home directory the default hides")
	return cmd
}

// logRedaction is what `shulker log` hides by default: the home directory, and every CurseForge key
// shulker may have sent.
func logRedaction() auditlog.Redaction {
	home, _ := os.UserHomeDir()
	var configured, cacheDir string
	if path, err := config.Path(); err == nil {
		if cfg, err := config.LoadFile(path); err == nil {
			configured = cfg.CurseForge.Key
		}
	}
	if c, err := cache.Open(); err == nil {
		cacheDir = c.Dir
	}
	return auditlog.Redaction{Home: home, Keys: curseforge.Keys(configured, cacheDir)}
}

// redact scrubs the report's entries, and the filter it names, which can hold an instance's path.
func redact(r *logReport, with auditlog.Redaction) {
	r.Redacted = true
	for i, e := range r.Entries {
		r.Entries[i] = with.Entry(e)
	}
	for name, value := range r.Filter {
		r.Filter[name] = with.String(value)
	}
}

var logFilterOrder = []string{"instance", "group", "cmd", "code", "level"}

// described is the filter as it ran, for the report to name.
func (f logFlags) described(instance string) map[string]string {
	described := map[string]string{}
	for i, value := range []string{instance, f.group, f.cmd, f.code, f.level} {
		if value != "" {
			described[logFilterOrder[i]] = value
		}
	}
	return described
}

func printLog(l *out.Lines, r logReport, isWidest bool) {
	t := l.T
	window := "last " + r.Since
	// A duration is never negative, so a dash is a date.
	if strings.Contains(r.Since, "-") {
		window = "since " + r.Since
	}
	redaction := t.Grey("redacted")
	if !r.Redacted {
		redaction = t.Yellow("unredacted")
	}
	l.Plain(t.Bold("shulker "+r.Version) + " " + t.Grey("("+r.Platform+", "+window+" of "+strconv.Itoa(r.KeepDays)+" days kept, ") + redaction + t.Grey(")"))
	var filter []string
	for _, name := range logFilterOrder {
		if value, ok := r.Filter[name]; ok {
			filter = append(filter, name+" "+value)
		}
	}
	described := "every entry"
	if len(filter) > 0 {
		described = strings.Join(filter, ", ")
	}
	l.Plain(described + " " + t.Grey(fmt.Sprintf("(%d of %d entries)", r.Matched, r.Read)))
	if len(r.Entries) > 0 {
		l.Blank()
		printLogEntries(l, r.Entries, r.labels)
	}
	if !isWidest {
		l.Nudge("Widen with", "shulker log --since "+strconv.Itoa(r.KeepDays)+"d")
	}
}

// printLogEntries is the table of entries: the level glyph in the first column, time, command and
// instance grey, then the event. An error's code is red with its message as the cell's second line.
// The instance column is left out when no entry names one.
func printLogEntries(l *out.Lines, entries []auditlog.Entry, labels []string) {
	t := l.T
	stamp := "15:04:05"
	today := time.Now().Format(time.DateOnly)
	named := false
	for _, e := range entries {
		if at, err := time.Parse(time.RFC3339Nano, e.At); err == nil && at.Local().Format(time.DateOnly) != today {
			stamp = "Jan 02 15:04:05"
		}
		named = named || e.Instance != ""
	}
	headers := []string{"", "Time", "Command", "Instance", "Event"}
	rows := make([][]string, len(entries))
	for i, e := range entries {
		at := e.At
		if parsed, err := time.Parse(time.RFC3339Nano, e.At); err == nil {
			at = parsed.Local().Format(stamp)
		}
		mark, event := "", logSummary(e)
		switch e.Level {
		case auditlog.LevelError:
			mark, event = t.GlyphError(), t.Red(cmp.Or(e.Code, "error"))
			if e.Msg != "" {
				event += "\n" + e.Msg
			}
		case auditlog.LevelWarn:
			mark, event = "!", e.Msg
		}
		rows[i] = []string{mark, at, logCmdName(e), cmp.Or(labels[i], e.Instance), event}
		if !named {
			rows[i] = slices.Delete(rows[i], 3, 4)
		}
	}
	if !named {
		headers = slices.Delete(headers, 3, 4)
	}
	l.Table(headers, rows, func(row, col int) lipgloss.Style {
		switch {
		case col == 0 && entries[row].Level == auditlog.LevelError:
			return t.StyleRed()
		case col == 0:
			return t.StyleYellow()
		case col < len(headers)-1:
			return t.StyleGrey()
		}
		return t.Style()
	})
}

// instanceLabels names the instance each entry acted on: a run logs the folder it started in, and
// the entries after it the instance's id, and both read as the instance's label.
func (a *app) instanceLabels(entries []auditlog.Entry) []string {
	labels := make([]string, len(entries))
	instances, err := a.loadInstances()
	if err != nil {
		return labels
	}
	for i, e := range entries {
		if e.Instance == "" {
			continue
		}
		n, ok := config.FindID(instances, e.Instance)
		if !ok {
			n, ok = config.FindInstance(instances, e.Instance)
		}
		if ok {
			labels[i] = instances[n].Label()
		}
	}
	return labels
}

// logCmdName is the command an entry names. The root's own runs, like a bare `shulker`, have none.
func logCmdName(e auditlog.Entry) string {
	return cmp.Or(e.Cmd, "shulker")
}

func logSummary(e auditlog.Entry) string {
	switch {
	case e.Msg == "start":
		return "run"
	case e.Msg == "end" && e.Exit != nil:
		took := ""
		if e.DurationMS != nil {
			took = " in " + (time.Duration(*e.DurationMS) * time.Millisecond).String()
		}
		return "exit " + strconv.Itoa(*e.Exit) + took
	}
	return e.Msg
}
