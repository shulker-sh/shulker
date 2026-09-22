package auditlog

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

const maxLine = 1 << 20

// Read is every entry in the log at path. A line that isn't an entry is skipped, and a log that
// isn't there has no entries.
func Read(path string) ([]Entry, error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 0, 64*1024), maxLine)
	var entries []Entry
	for scan.Scan() {
		var e Entry
		if json.Unmarshal(scan.Bytes(), &e) == nil && e.At != "" {
			entries = append(entries, e)
		}
	}
	return entries, scan.Err()
}

// Filter picks entries out of the log. A zero field admits every entry.
type Filter struct {
	Since time.Time
	Group string
	// Cmd is a command path, which also admits the commands under it.
	Cmd   string
	Code  string
	Level string
	// Instance is every name one instance goes by: an entry names it by whichever the run was given.
	Instance []string
}

// Match reports whether the filter admits e.
func (f Filter) Match(e Entry) bool {
	if !f.Since.IsZero() {
		at, err := time.Parse(time.RFC3339Nano, e.At)
		if err != nil || at.Before(f.Since) {
			return false
		}
	}
	if f.Cmd != "" && e.Cmd != f.Cmd && !strings.HasPrefix(e.Cmd, f.Cmd+" ") {
		return false
	}
	if len(f.Instance) > 0 && !slices.Contains(f.Instance, e.Instance) {
		return false
	}
	return (f.Group == "" || e.Group == f.Group) && (f.Code == "" || e.Code == f.Code) && (f.Level == "" || e.Level == f.Level)
}

// ParseSince is the start of a window given as a duration back from now (24h, 90m, 7d) or as a
// date or time, which a date without a zone reads in local time.
func ParseSince(s string, now time.Time) (time.Time, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		if n, err := strconv.Atoi(days); err == nil && n > 0 {
			return now.AddDate(0, 0, -n), nil
		}
	}
	if d, err := time.ParseDuration(s); err == nil && d > 0 {
		return now.Add(-d), nil
	}
	if at, err := time.ParseInLocation(time.DateOnly, s, time.Local); err == nil {
		return at, nil
	}
	if at, err := time.Parse(time.RFC3339, s); err == nil {
		return at, nil
	}
	return time.Time{}, errors.New("neither a duration nor a date")
}
