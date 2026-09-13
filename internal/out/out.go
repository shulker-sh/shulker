package out

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
)

const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
	// ExitInterrupted follows the shell's 128+SIGINT.
	ExitInterrupted = 130
)

type Envelope struct {
	OK        bool     `json:"ok"`
	Command   string   `json:"command"`
	LockStale bool     `json:"lockStale"`
	Warnings  []string `json:"warnings"`
	Data      any      `json:"data,omitempty"`
	Error     *Error   `json:"error,omitempty"`
}

type Error struct {
	Code       string   `json:"code"`
	Message    string   `json:"message"`
	Candidates []string `json:"candidates,omitempty"`
	Items      []string `json:"items,omitempty"`
	Exit       int      `json:"-"`
	// Data is the partial result of a command that failed part-way; it goes in the envelope's data.
	Data any `json:"-"`
	// Help is the human-only "help:" row under the error line.
	Help string `json:"-"`
	// Nudge is the human-only command to run next, with its lead-in.
	Nudge Nudge `json:"-"`
	// Given is the argument the user typed that a pick replaces in the example command.
	Given string `json:"-"`
	// Flag receives the pick in the example command when no typed argument is replaced.
	Flag string `json:"-"`
	// Pass is what to type for each candidate, when that differs from how it reads.
	Pass []string `json:"-"`
	// Rows are the human-only tree rows under the error line. Without them the
	// items show, and without those the message's remaining lines.
	Rows []Detail `json:"-"`
}

// Detail is one row under an error line; Children nest one level beneath it.
// Command marks Text as something to type.
type Detail struct {
	Label    string
	Text     string
	Command  bool
	Children []Detail
}

type Nudge struct {
	Lead    string
	Command string
}

func (e *Error) Error() string { return e.Message }

func Errorf(code string, format string, args ...any) *Error {
	exit := ExitError
	if code == "usage" {
		exit = ExitUsage
	}
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), Exit: exit}
}

func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func AsError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		if e.Exit == 0 {
			e.Exit = ExitError
		}
		return e
	}
	return &Error{Code: "error", Message: err.Error(), Exit: ExitError}
}

type Printer struct {
	JSON      bool
	Command   string
	LockStale bool
	Stdout    io.Writer
	Stderr    io.Writer
	// Args is the command line as typed, for example commands under errors.
	Args []string
	// WarnPrefix names the target or instance a multi-part run is on.
	WarnPrefix string
	warnings   []string
	Theme      Theme
	ErrTheme   Theme
}

// Out is the results stream; Err carries warnings, errors, and progress.
func (p *Printer) Out() *Lines { return &Lines{W: p.Stdout, T: p.Theme} }
func (p *Printer) Err() *Lines { return &Lines{W: p.Stderr, T: p.ErrTheme} }

func (p *Printer) Warn(format string, args ...any) {
	msg := p.WarnPrefix + fmt.Sprintf(format, args...)
	if slices.Contains(p.warnings, msg) {
		return
	}
	p.warnings = append(p.warnings, msg)
	if !p.JSON {
		text := fmt.Sprintf(format, args...)
		if p.WarnPrefix != "" {
			text = p.ErrTheme.Grey(p.WarnPrefix) + text
		}
		p.Err().Warn(text)
	}
}

func (p *Printer) envelope(ok bool, data any, e *Error) Envelope {
	warnings := p.warnings
	if warnings == nil {
		warnings = []string{}
	}
	return Envelope{OK: ok, Command: p.Command, LockStale: p.LockStale, Warnings: warnings, Data: data, Error: e}
}

func (p *Printer) Emit(data any, human func(l *Lines)) error {
	if p.JSON {
		return p.encode(p.envelope(true, data, nil))
	}
	human(p.Out())
	return nil
}

func (p *Printer) Fail(err error) int {
	e := AsError(err)
	if p.JSON {
		_ = p.encode(p.envelope(false, e.Data, e))
		return e.Exit
	}
	if e.Nudge.Command == "" {
		if _, picks := e.picks(); len(picks) > 0 {
			if command, ok := exampleCommand(p.Args, e, picks[0].Pass); ok {
				e.Nudge = Nudge{Lead: "For example", Command: command}
			}
		}
	}
	p.Err().Error(e)
	return e.Exit
}

func (p *Printer) encode(v any) error {
	enc := json.NewEncoder(p.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
