package out

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type stepState struct {
	mu      sync.Mutex
	running *step
	shown   []string
}

type step struct {
	text    string
	tty     *os.File
	wheel   spinner.Model
	stop    chan struct{}
	stopped chan struct{}
}

// newSpinner is the cyan spinner every running line shares, driven by the caller's own clock
// through Update. bubbles' Dot frames end in a space, which is trimmed so one space sits before
// the text.
func newSpinner(t Theme) spinner.Model {
	kind := spinner.Dot
	if t.ASCII {
		kind = spinner.Line
	}
	frames := make([]string, len(kind.Frames))
	for i, f := range kind.Frames {
		frames[i] = strings.TrimSpace(f)
	}
	kind.Frames = frames
	return spinner.New(spinner.WithSpinner(kind), spinner.WithStyle(t.Style().Foreground(lipgloss.Color("6")).Bold(true)))
}

// slowAfter is how long a step runs before its spinner says how long it has waited, and on what.
const slowAfter = 3 * time.Second

// waits has its own lock because settling a step holds the step lock while the spinner, which
// reads waits, finishes its last frame.
type waits struct {
	mu    sync.Mutex
	hosts []string
}

// Waiting records a request in flight to host, which a slow step's spinner names; done ends it.
func (p *Printer) Waiting(host string) (done func()) {
	w := &p.waits
	w.mu.Lock()
	w.hosts = append(w.hosts, host)
	w.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			w.mu.Lock()
			defer w.mu.Unlock()
			if i := slices.Index(w.hosts, host); i >= 0 {
				w.hosts = slices.Delete(w.hosts, i, i+1)
			}
		})
	}
}

func (w *waits) latest() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.hosts) == 0 {
		return ""
	}
	return w.hosts[len(w.hosts)-1]
}

func slowAside(elapsed time.Duration, host string) string {
	if elapsed < slowAfter {
		return ""
	}
	seconds := int(elapsed / time.Second)
	if host == "" {
		return fmt.Sprintf(" (%ds)", seconds)
	}
	return fmt.Sprintf(" (%ds, waiting on %s)", seconds, host)
}

// Step shows a piece of work under way. On a terminal it spins until the next step or any other
// output, then settles into a grey ok line in the past tense; off a terminal only that line prints.
// A step already shown in this run is skipped.
func (p *Printer) Step(format string, args ...any) {
	if p.JSON {
		return
	}
	text := OneLine(fmt.Sprintf(format, args...))
	p.steps.mu.Lock()
	defer p.steps.mu.Unlock()
	if slices.Contains(p.steps.shown, text) {
		return
	}
	p.steps.shown = append(p.steps.shown, text)
	p.settleLocked(true)
	p.open(p.Stderr)
	if verb, _, _ := strings.Cut(text, " "); !strings.HasSuffix(verb, "ing") {
		(&Lines{W: p.Stderr, T: p.ErrTheme}).Done(Sentence(text))
		return
	}
	s := &step{text: text}
	if f, ok := p.Stderr.(*os.File); ok && IsTerminal(f) {
		s.tty = f
		s.wheel = newSpinner(p.ErrTheme)
		s.stop, s.stopped = make(chan struct{}), make(chan struct{})
		go s.spin(p.ErrTheme, &p.waits)
	}
	p.steps.running = s
}

// Settle finishes the running step. Anything written through Out or Err settles first; code that
// writes to Stderr directly must call it.
func (p *Printer) Settle() { p.settle(true) }

// Drop clears the running step without a done line, for work that failed and was recovered from:
// the warning that follows says what happened.
func (p *Printer) Drop() { p.settle(false) }

func (p *Printer) settle(done bool) {
	p.steps.mu.Lock()
	defer p.steps.mu.Unlock()
	p.settleLocked(done)
}

func (p *Printer) settleLocked(done bool) {
	s := p.steps.running
	if s == nil {
		return
	}
	p.steps.running = nil
	if s.tty != nil {
		close(s.stop)
		<-s.stopped
		fmt.Fprint(s.tty, "\r\x1b[J")
	}
	if done {
		(&Lines{W: p.Stderr, T: p.ErrTheme}).Done(Sentence(settledText(s.text)))
	}
}

func (s *step) spin(t Theme, w *waits) {
	defer close(s.stopped)
	tick := time.NewTicker(frameEvery)
	defer tick.Stop()
	start := time.Now()
	for {
		room := terminalWidth(s.tty) - len(gutter) - 3
		text := Sentence(s.text) + slowAside(time.Since(start), w.latest())
		fmt.Fprint(s.tty, "\r\x1b[J"+gutter+s.wheel.View()+" "+t.Grey(ansi.Truncate(text, room, t.Ellipsis())))
		select {
		case <-s.stop:
			return
		case <-tick.C:
			s.wheel, _ = s.wheel.Update(spinner.TickMsg{})
		}
	}
}

func settledText(text string) string {
	verb, rest, ok := strings.Cut(text, " ")
	if !ok {
		return pastTense(verb)
	}
	return pastTense(verb) + " " + rest
}

type settling struct {
	p *Printer
	w io.Writer
}

func (s settling) Write(b []byte) (int, error) {
	s.p.Settle()
	s.p.open(s.w)
	return s.w.Write(b)
}
