// Package out is how a command speaks: the JSON envelope and the human lines, errors with their
// codes, help and picks, warnings and nudges, progress steps and the terminal they print on.
package out

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	ExitOK    = 0
	ExitError = 1
	ExitUsage = 2
	// ExitInterrupted follows the shell's 128+SIGINT.
	ExitInterrupted = 130
)

// Envelope is what every command prints under --json, whether it succeeded or failed.
type Envelope struct {
	OK        bool     `json:"ok"`
	Command   string   `json:"command"`
	LockStale bool     `json:"lockStale"`
	Warnings  []string `json:"warnings"`
	Data      any      `json:"data,omitempty"`
	Error     *Error   `json:"error,omitempty"`
}

// Error is a failure the user sees. Code is its stable name, matched by code and never by message.
type Error struct {
	Code       string   `json:"code"`
	Message    string   `json:"message"`
	Candidates []string `json:"candidates,omitempty"`
	Items      []string `json:"items,omitempty"`
	Exit       int      `json:"-"`
	// Data is the partial result of a command that failed part-way; it goes in the envelope's data.
	Data any `json:"-"`
	// Help is what to do about the error: the "help:" row under a human error line, and help in JSON.
	Help string `json:"help,omitempty"`
	// Cause is the reason in the parser's or checker's own words, without the file's name, for a
	// reader that warns with it and goes on rather than failing.
	Cause error `json:"-"`
	// Nudge is the human-only command to run next, with its lead-in.
	Nudge Nudge `json:"-"`
	// IsPlain marks an error rendered as dialog body text: no glyph, no code aside, no gutter, and the
	// nudge's command without its prompt. A launcher shows what a hook wrote with no terminal
	// around it, where that decoration reads as noise.
	IsPlain bool `json:"-"`
	// Given is the argument the user typed that a pick replaces in the example command.
	Given string `json:"-"`
	// Flag receives the pick in the example command when no typed argument is replaced.
	Flag string `json:"-"`
	// Setting is the shulker.json key the pick is the value of, for a command without Flag: the
	// example is then `shulker set <Setting> <pick>`.
	Setting string `json:"-"`
	// Pass is what to type for each candidate, when that differs from how it reads.
	Pass []string `json:"-"`
	// Rows are the human-only tree rows under the error line. Without them the
	// items show, and without those the message's remaining lines.
	Rows []Detail `json:"-"`
	// Usage prints under a human error that suggests nothing else to run.
	Usage func(l *Lines) `json:"-"`
	// IsSummary marks an error whose items repeat errors the run already reported, so it adds no
	// annotations of its own.
	IsSummary bool `json:"-"`
	// Wrapped is the error this one explains, which errors.Is and errors.As see through to.
	Wrapped error `json:"-"`
	// isNamed marks a message that opens with a name, such as a mod's slug, which keeps its case
	// when the line is capitalized.
	isNamed bool
}

// Detail is one row under an error line; Children nest one level beneath it.
// IsCommand marks Text as something to type.
type Detail struct {
	Label     string
	Text      string
	IsCommand bool
	Children  []Detail
}

type Nudge struct {
	Lead    string
	Command string
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.Wrapped }

// Headline is the message's first line, without the colon that introduces the lines under it.
func (e *Error) Headline() string {
	headline, _, _ := strings.Cut(e.Message, "\n")
	return strings.TrimSuffix(headline, ":")
}

// WithCause adds err as a row labelled by what produced it (`json`, `zip`, a service's name), so the
// headline says only what went wrong.
func (e *Error) WithCause(label string, err error) *Error {
	e.Rows = append(e.Rows, Detail{Label: label, Text: err.Error()})
	return e
}

func Errorf(code string, format string, args ...any) *Error {
	exit := ExitError
	if code == "usage" {
		exit = ExitUsage
	}
	return &Error{Code: code, Message: fmt.Sprintf(format, args...), Exit: exit, isNamed: format != "%s" && opensWithValue(format)}
}

// Sentence capitalizes text's first letter, for a line whose first word is prose. A first word
// that reads as a name, such as shulker.json, fabric-api or a `name:` label, keeps its case.
func Sentence(text string) string {
	r, size := utf8.DecodeRuneInString(text)
	if !unicode.IsLower(r) {
		return text
	}
	word, _, _ := strings.Cut(text, " ")
	if strings.ContainsAny(word, "._-/:0123456789") {
		return text
	}
	return string(unicode.ToUpper(r)) + text[size:]
}

// opensWithValue reports whether a format starts with a substituted value, which keeps its own
// case: `%s is already in the pack` names a mod.
func opensWithValue(format string) bool { return strings.HasPrefix(format, "%") }

func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// AsError is err as an *Error, giving one that carries no code the generic code "error".
func AsError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		if e.Exit == 0 {
			e.Exit = ExitError
		}
		return e
	}
	return &Error{Code: "error", Message: err.Error(), Exit: ExitError, isNamed: true}
}

// Recorder is told every warning, error and result a run shows, whether it prints for a person or
// as JSON.
type Recorder interface {
	Warn(msg string)
	Error(code, msg string)
	Result(data any)
}

// Printer is one run's output: JSON or human, never both, on the run's two streams.
type Printer struct {
	JSON bool
	// NoInput is --no-input: the run asks nothing, so a prompt takes its default and a required
	// value left unset is a usage error instead.
	NoInput   bool
	Command   string
	LockStale bool
	Stdout    io.Writer
	Stderr    io.Writer
	// Args is the command line as typed, for example commands under errors.
	Args []string
	// WarnPrefix names the side or instance a multi-part run is on.
	WarnPrefix string
	// Recorder, when set, hears each warning once, the way the run shows it, and each result as the
	// data --json prints.
	Recorder Recorder
	// Annotate prints each warning and error as a GitHub Actions workflow command as well, which
	// the runner reads from either stream and shows as an annotation on the run.
	Annotate bool
	// Framed decides which streams get the blank lines around a run's output; unset, every
	// terminal does.
	Framed func(io.Writer) bool
	// ClearFetches makes a fetching or looking-up step a line that clears when it ends instead of
	// settling, for a command whose result rows already say what it fetched.
	ClearFetches bool
	frame        frame
	warnings     []string
	// printed is whether stdout already holds the run's output: an envelope, or what a Raw command
	// wrote itself.
	printed  bool
	steps    stepState
	waits    waits
	Theme    Theme
	ErrTheme Theme
}

// Out is the results stream; Err carries warnings, errors, and progress.
func (p *Printer) Out() *Lines { return &Lines{W: settling{p, p.Stdout}, T: p.Theme} }
func (p *Printer) Err() *Lines { return &Lines{W: settling{p, p.Stderr}, T: p.ErrTheme} }

func (p *Printer) Warn(format string, args ...any) {
	p.warn(format, args...)
}

// WarnNudge is Warn with the command that deals with the warning printed beneath it, the way an
// error's nudge is.
func (p *Printer) WarnNudge(n Nudge, format string, args ...any) {
	if p.warn(format, args...) && !p.JSON {
		l := p.Err()
		l.Nudge(n.Lead, n.Command)
		l.Blank()
	}
}

// warn reports whether the warning was new; a repeat is dropped.
func (p *Printer) warn(format string, args ...any) bool {
	msg := p.WarnPrefix + fmt.Sprintf(format, args...)
	if slices.Contains(p.warnings, msg) {
		return false
	}
	p.warnings = append(p.warnings, msg)
	if p.Recorder != nil {
		p.Recorder.Warn(msg)
	}
	p.annotate("warning", "", msg)
	if !p.JSON {
		text := fmt.Sprintf(format, args...)
		if !opensWithValue(format) {
			text = Sentence(text)
		}
		if p.WarnPrefix != "" {
			text = p.ErrTheme.Grey(p.WarnPrefix) + text
		}
		p.Err().Warn(text)
	}
	return true
}

func (p *Printer) envelope(ok bool, data any, e *Error) Envelope {
	warnings := p.warnings
	if warnings == nil {
		warnings = []string{}
	}
	return Envelope{OK: ok, Command: p.Command, LockStale: p.LockStale, Warnings: warnings, Data: data, Error: e}
}

// Emit prints a command's result: data as the JSON envelope under --json, otherwise whatever human
// writes.
func (p *Printer) Emit(data any, human func(l *Lines)) error {
	if p.Recorder != nil {
		p.Recorder.Result(data)
	}
	if p.JSON {
		return p.encode(p.envelope(true, data, nil))
	}
	human(p.Out())
	return nil
}

// Report shows one failure of a run that goes on to the next part, such as one instance of several.
// Under --json it prints nothing, since the failure is in the command's own data.
func (p *Printer) Report(e *Error) {
	p.record(e)
	if !p.JSON {
		p.Err().Error(e)
	}
}

func (p *Printer) record(e *Error) {
	if p.Recorder != nil {
		p.Recorder.Error(e.Code, e.Message)
	}
	if e.IsSummary {
		return
	}
	headline := e.Headline()
	code := cmp.Or(e.Code, "error")
	if len(e.Items) == 0 {
		p.annotate("error", code, headline)
	}
	for _, item := range e.Items {
		p.annotate("error", headline+" ("+code+")", item)
	}
}

// annotate prints one GitHub Actions workflow command, escaped as the runner unescapes it.
func (p *Printer) annotate(level, title, message string) {
	if !p.Annotate {
		return
	}
	command := "::" + level
	if title != "" {
		command += " title=" + annotationEscaper(true).Replace(title)
	}
	fmt.Fprintf(settling{p, p.Stderr}, "%s::%s\n", command, annotationEscaper(false).Replace(message))
}

func annotationEscaper(isProperty bool) *strings.Replacer {
	pairs := []string{"%", "%25", "\r", "%0D", "\n", "%0A"}
	if isProperty {
		pairs = append(pairs, ":", "%3A", ",", "%2C")
	}
	return strings.NewReplacer(pairs...)
}

// Fail prints err as the run's error and returns the exit code the run ends with.
func (p *Printer) Fail(err error) int {
	p.settle(false)
	e := AsError(err)
	p.record(e)
	if p.JSON {
		_ = p.encode(p.envelope(false, e.Data, e))
		return e.Exit
	}
	picked := false
	if e.Nudge.Command == "" {
		if _, picks := e.picks(); len(picks) > 0 {
			picked = true
			if command, ok := exampleCommand(p.Args, e, picks[0].Pass); ok {
				e.Nudge = Nudge{Lead: "For example", Command: command}
			}
		}
	}
	p.Err().Error(e)
	if e.Usage != nil && !picked && e.Nudge.Command == "" {
		e.Usage(p.Err())
	}
	p.close()
	return e.Exit
}

// Raw marks the run's stdout as the command's own output, such as a completion script, so Finish
// prints no envelope after it.
func (p *Printer) Raw() { p.printed = true }

// Finish ends a run that succeeded. Under --json, a command that emitted nothing still prints an
// envelope, so its warnings reach the caller.
func (p *Printer) Finish() {
	p.Settle()
	if p.JSON && !p.printed {
		_ = p.encode(p.envelope(true, nil, nil))
	}
	p.close()
}

func (p *Printer) encode(v any) error {
	p.printed = true
	enc := json.NewEncoder(p.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Count writes n with the noun that agrees with it: "1 file", "2 files".
func Count(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
