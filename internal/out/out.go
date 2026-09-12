package out

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
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
	// WarnPrefix names the target or instance a multi-part run is on.
	WarnPrefix string
	warnings   []string
}

func (p *Printer) Warn(format string, args ...any) {
	msg := p.WarnPrefix + fmt.Sprintf(format, args...)
	if slices.Contains(p.warnings, msg) {
		return
	}
	p.warnings = append(p.warnings, msg)
	if !p.JSON {
		fmt.Fprintf(p.Stderr, "warning: %s\n", msg)
	}
}

func (p *Printer) envelope(ok bool, data any, e *Error) Envelope {
	warnings := p.warnings
	if warnings == nil {
		warnings = []string{}
	}
	return Envelope{OK: ok, Command: p.Command, LockStale: p.LockStale, Warnings: warnings, Data: data, Error: e}
}

func (p *Printer) Emit(data any, human func(w io.Writer)) error {
	if p.JSON {
		return p.encode(p.envelope(true, data, nil))
	}
	human(p.Stdout)
	return nil
}

func (p *Printer) Fail(err error) int {
	e := AsError(err)
	if p.JSON {
		_ = p.encode(p.envelope(false, e.Data, e))
		return e.Exit
	}
	code := e.Code
	if code == "" {
		code = "error"
	}
	fmt.Fprintf(p.Stderr, "shulker: %s (%s)\n", e.Message, code)
	if len(e.Candidates) > 0 {
		fmt.Fprintf(p.Stderr, "  candidates: %s\n", strings.Join(e.Candidates, ", "))
	}
	for _, item := range e.Items {
		if !strings.Contains(e.Message, item) {
			fmt.Fprintf(p.Stderr, "  - %s\n", item)
		}
	}
	return e.Exit
}

func (p *Printer) encode(v any) error {
	enc := json.NewEncoder(p.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
