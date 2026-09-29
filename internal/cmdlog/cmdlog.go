// Package cmdlog appends what shulker runs did to log.jsonl, one JSON entry per line: the run's
// start and end, the result it reported, and every warning and error it showed.
package cmdlog

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"shulker.sh/shulker/internal/fsutil"
)

// FileName is the log's name in the config folder, beside config.json.
const FileName = "log.jsonl"

// The levels an entry takes: a run's start and end are info, the rest what the printer showed.
const (
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

// ErrNoPath is a run that has nowhere to keep its log, because the config folder can't be found.
var ErrNoPath = errors.New("no config folder to keep " + FileName + " in")

// refused stands in for a piece of the game's argv wherever one turns up in an entry.
const refused = "[argv]"

// Shorter pieces of argv are values like "msa" or "26.2" that read as ordinary words inside a
// message, and none of them is a secret.
const minRefused = 8

// Entry is one line of the log.
type Entry struct {
	At         string            `json:"at"`
	Group      string            `json:"group"`
	Cmd        string            `json:"cmd"`
	Instance   string            `json:"instance,omitempty"`
	Level      string            `json:"level"`
	Code       string            `json:"code,omitempty"`
	Msg        string            `json:"msg"`
	Flags      map[string]string `json:"flags,omitempty"`
	Exit       *int              `json:"exit,omitempty"`
	DurationMS *int64            `json:"durationMs,omitempty"`
	Data       json.RawMessage   `json:"data,omitempty"`
}

// Log writes one run's entries. Cmd, Group and Instance go on every entry; Instance may be filled in
// once the run has resolved it.
type Log struct {
	Path     string
	Cmd      string
	Group    string
	Instance string
	Now      func() time.Time
	// ReadOnly is a run that changes nothing, which is kept only if it goes wrong: its start waits
	// for its first warning or error, and its result is never written.
	ReadOnly bool
	// OnFail hears the first write that fails. Nothing is written after it, so it is heard once.
	OnFail func(error)
	// Opened hears the run's first entry land in the log, once.
	Opened  func()
	argv    []string
	started time.Time
	held    *Entry
	opened  bool
	err     error
}

// New is the log for a run with the command line args. Everything after the first "--" is argv
// handed on to another program, which for `hook wrap` carries the game's session access token, so
// no piece of it is ever written, whichever entry or field it turns up in. A piece shulker was also
// given before the "--", like the instance directory, is shulker's own and stays.
func New(path string, args []string) *Log {
	l := &Log{Path: path, Now: time.Now}
	dash := slices.Index(args, "--")
	if dash < 0 {
		return l
	}
	for _, arg := range args[dash+1:] {
		if len(arg) >= minRefused && !slices.Contains(args[:dash], arg) {
			l.argv = append(l.argv, arg)
		}
	}
	return l
}

// Start opens the run, which End measures from.
func (l *Log) Start(cmd, group, instance string, flags map[string]string) {
	l.Cmd, l.Group, l.Instance = cmd, group, instance
	l.started = l.Now()
	start := Entry{At: stamp(l.started), Level: LevelInfo, Msg: "start", Flags: flags}
	if l.ReadOnly {
		l.held = &start
		return
	}
	l.append(start)
}

// Acts turns a read-only run into one that changes something, which is logged from its start.
func (l *Log) Acts() {
	l.ReadOnly = false
	l.release()
}

func (l *Log) release() {
	if l.held != nil {
		start := *l.held
		l.held = nil
		l.append(start)
	}
}

// Warn records a warning the run showed.
func (l *Log) Warn(msg string) {
	l.release()
	l.append(Entry{Level: LevelWarn, Msg: msg})
}

// Error records an error the run showed, with its code.
func (l *Log) Error(code, msg string) {
	l.release()
	l.append(Entry{Level: LevelError, Code: code, Msg: msg})
}

// Result records the payload the run reported, as --json prints it under "data".
func (l *Log) Result(data any) {
	if l.err != nil || l.ReadOnly {
		return
	}
	payload, err := encode(data)
	if err != nil {
		l.fail(err)
		return
	}
	l.append(Entry{Level: LevelInfo, Msg: "result", Data: payload})
}

func encode(v any) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	err := enc.Encode(v)
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")), err
}

// End closes the run with its exit code and how long it took. A read-only run that went right has
// nothing to close.
func (l *Log) End(exit int) {
	if l.held != nil {
		return
	}
	ms := l.Now().Sub(l.started).Milliseconds()
	l.append(Entry{Level: LevelInfo, Msg: "end", Exit: &exit, DurationMS: &ms})
}

func (l *Log) append(e Entry) {
	if l.err != nil {
		return
	}
	if e.At == "" {
		e.At = stamp(l.Now())
	}
	e.Cmd, e.Group, e.Instance = l.Cmd, l.Group, l.Instance
	l.refuseArgv(&e)
	line, err := encode(e)
	if err == nil {
		err = appendFile(l.Path, append(line, '\n'))
	}
	if err != nil {
		l.fail(err)
		return
	}
	if !l.opened {
		l.opened = true
		if l.Opened != nil {
			l.Opened()
		}
	}
}

func stamp(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}

func (l *Log) fail(err error) {
	l.err = err
	if l.OnFail != nil {
		l.OnFail(err)
	}
}

func (l *Log) refuseArgv(e *Entry) {
	if len(l.argv) == 0 {
		return
	}
	refuse := func(s string) string {
		for _, arg := range l.argv {
			s = strings.ReplaceAll(s, arg, refused)
		}
		return s
	}
	*e = e.rewrite(refuse)
}

// rewrite is e with f applied to every string it holds, the flags' names and the payload's keys
// included.
func (e Entry) rewrite(f func(string) string) Entry {
	e.Cmd, e.Group, e.Instance, e.Code, e.Msg = f(e.Cmd), f(e.Group), f(e.Instance), f(e.Code), f(e.Msg)
	if e.Flags != nil {
		flags := make(map[string]string, len(e.Flags))
		for name, value := range e.Flags {
			flags[f(name)] = f(value)
		}
		e.Flags = flags
	}
	if e.Data != nil {
		e.Data = rewriteData(e.Data, f)
	}
	return e
}

// rewriteData applies f to every string of a payload by decoding it: a replace over the raw JSON
// could cut into an escape sequence. A payload it can't read is dropped.
func rewriteData(raw json.RawMessage, f func(string) string) json.RawMessage {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var data any
	if err := dec.Decode(&data); err != nil {
		return nil
	}
	rewritten, err := encode(rewriteValue(data, f))
	if err != nil {
		return nil
	}
	return rewritten
}

func rewriteValue(v any, f func(string) string) any {
	switch v := v.(type) {
	case string:
		return f(v)
	case []any:
		for i, item := range v {
			v[i] = rewriteValue(item, f)
		}
		return v
	case map[string]any:
		kept := make(map[string]any, len(v))
		for key, item := range v {
			kept[f(key)] = rewriteValue(item, f)
		}
		return kept
	}
	return v
}

// appendFile adds data to the end of path in one write, so entries from runs appending at the same
// time never interleave. The file holds unredacted detail, so only its owner reads it.
func appendFile(path string, data []byte) error {
	if path == "" {
		return ErrNoPath
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	unlock, err := fsutil.Lock(lockPath(path))
	if err != nil {
		return err
	}
	defer unlock()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
