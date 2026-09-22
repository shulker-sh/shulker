package cli

import (
	"cmp"
	"fmt"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/auditlog"
	"shulker.sh/shulker/internal/out"
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
	Entries  []auditlog.Entry  `json:"entries"`
}

type logFlags struct {
	since, group, cmd, code, level string
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
				return out.Errorf("usage", "--since: %s", err)
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
				Version:  version,
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
	return cmd
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
	dot := " " + t.GlyphDot() + " "
	window := "last " + r.Since
	// A duration is never negative, so a dash is a date.
	if strings.Contains(r.Since, "-") {
		window = "since " + r.Since
	}
	l.Plain(t.Bold("shulker "+r.Version) + t.Grey(dot+r.Platform+dot+window+" of "+strconv.Itoa(r.KeepDays)+" days kept"))
	var filter []string
	for _, name := range logFilterOrder {
		if value, ok := r.Filter[name]; ok {
			filter = append(filter, name+" "+value)
		}
	}
	described := "every entry"
	if len(filter) > 0 {
		described = strings.Join(filter, dot)
	}
	l.Plain(described + t.Grey(fmt.Sprintf(" — %d of %d entries", r.Matched, r.Read)))
	if len(r.Entries) > 0 {
		l.Blank()
		printLogEntries(l, r.Entries)
	}
	if !isWidest {
		l.Nudge("Widen with", "shulker log --since "+strconv.Itoa(r.KeepDays)+"d")
	}
}

func printLogEntries(l *out.Lines, entries []auditlog.Entry) {
	t := l.T
	stamp := "15:04:05"
	today := time.Now().Format(time.DateOnly)
	cmdWidth, instanceWidth := 0, 0
	for _, e := range entries {
		if at, err := time.Parse(time.RFC3339Nano, e.At); err == nil && at.Local().Format(time.DateOnly) != today {
			stamp = "Jan 02 15:04:05"
		}
		cmdWidth, instanceWidth = max(cmdWidth, out.Width(logCmdName(e))), max(instanceWidth, out.Width(e.Instance))
	}
	for _, e := range entries {
		at := e.At
		if parsed, err := time.Parse(time.RFC3339Nano, e.At); err == nil {
			at = parsed.Local().Format(stamp)
		}
		lead := pad(at, len(stamp)) + "  " + pad(logCmdName(e), cmdWidth) + "  "
		if instanceWidth > 0 {
			lead += pad(e.Instance, instanceWidth) + "  "
		}
		switch e.Level {
		case auditlog.LevelError:
			l.Raw(t.Red(t.GlyphError()) + " " + t.Grey(lead) + t.Red(cmp.Or(e.Code, "error")))
			if e.Msg != "" {
				l.Raw("  " + strings.Repeat(" ", out.Width(lead)) + e.Msg)
			}
		case auditlog.LevelWarn:
			l.Raw(t.Yellow("!") + " " + t.Grey(lead) + e.Msg)
		default:
			l.Plain(t.Grey(lead) + logSummary(e))
		}
	}
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

func pad(s string, width int) string {
	return s + strings.Repeat(" ", max(0, width-out.Width(s)))
}
