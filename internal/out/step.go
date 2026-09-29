package out

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/spinner"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
)

type stepState struct {
	mu      sync.Mutex
	running *step
	shown   []string
	// groups are the progress groups that settled into a line, by groupKey.
	groups []string
	// bar is the download bar drawing on the terminal, which every other line clears first.
	bar *Progress
}

type step struct {
	text string
	// clears ends the step without a done line, for work whose result says what it did.
	clears  bool
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
	return spinner.New(spinner.WithSpinner(kind), spinner.WithStyle(t.StyleCommand()))
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
	text := OneLine(fmt.Sprintf(format, args...))
	verb, _, _ := strings.Cut(text, " ")
	p.step(text, p.ClearFetches && (verb == "fetching" || verb == "looking"))
}

// Working shows a piece of work under way on a terminal, like Step, and clears it when it ends:
// the result that follows says what it did. Off a terminal it prints nothing.
func (p *Printer) Working(format string, args ...any) {
	p.step(OneLine(fmt.Sprintf(format, args...)), true)
}

// Pending shows something waited on as a grey pending line. On a terminal it is a live line
// without a spinner, which the next output replaces in place, as a finished wait's result does;
// off a terminal it prints and stays.
func (p *Printer) Pending(text string) {
	if p.JSON {
		return
	}
	p.steps.mu.Lock()
	defer p.steps.mu.Unlock()
	p.settleLocked(true)
	p.open(p.Stderr)
	f, isTTY := p.Stderr.(*os.File)
	if !isTTY || !IsTerminal(f) || p.steps.bar != nil {
		(&Lines{W: p.errLocked(), T: p.ErrTheme}).Pending(text)
		return
	}
	t := p.ErrTheme
	room := terminalWidth(f) - len(gutter) - 3
	fmt.Fprint(f, "\r\x1b[J"+gutter+t.Grey(t.GlyphPending()+" "+ansi.Truncate(text, room, t.Ellipsis())))
	p.steps.running = &step{text: text, clears: true, tty: f}
}

func (p *Printer) step(text string, clears bool) {
	if p.JSON {
		return
	}
	p.steps.mu.Lock()
	defer p.steps.mu.Unlock()
	if slices.Contains(p.steps.shown, text) {
		return
	}
	p.steps.shown = append(p.steps.shown, text)
	p.settleLocked(true)
	verb, _, _ := strings.Cut(text, " ")
	f, isTTY := p.Stderr.(*os.File)
	// A spinner under a live bar would fight it for the row, so the step only settles.
	isTTY = isTTY && IsTerminal(f) && p.steps.bar == nil
	if clears && !isTTY {
		return
	}
	p.open(p.Stderr)
	if !strings.HasSuffix(verb, "ing") {
		(&Lines{W: p.errLocked(), T: p.ErrTheme}).Done(Sentence(text))
		return
	}
	s := &step{text: text, clears: clears}
	if isTTY {
		s.tty = f
		s.wheel = newSpinner(p.ErrTheme)
		s.stop, s.stopped = make(chan struct{}), make(chan struct{})
		go s.spin(p.ErrTheme, &p.waits)
	}
	p.steps.running = s
}

// Note prints it as a list row among the steps: a remark about one entry rather than work done.
func (p *Printer) Note(it Item) {
	if !p.JSON {
		p.Err().Items(it)
	}
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
		if s.stop != nil {
			close(s.stop)
			<-s.stopped
		}
		fmt.Fprint(s.tty, "\r\x1b[J")
	}
	if done && !s.clears {
		(&Lines{W: p.errLocked(), T: p.ErrTheme}).Done(Sentence(settledText(s.text)))
	}
}

func (s *step) spin(t Theme, w *waits) {
	defer close(s.stopped)
	tick := time.NewTicker(frameEvery)
	defer tick.Stop()
	start := time.Now()
	frames := &colorprofile.Writer{Forward: s.tty, Profile: t.Profile()}
	for {
		room := terminalWidth(s.tty) - len(gutter) - 3
		text := Tilde(Sentence(s.text)) + slowAside(time.Since(start), w.latest())
		fmt.Fprint(frames, "\r\x1b[J"+gutter+s.wheel.View()+" "+t.Grey(ansi.Truncate(text, room, t.Ellipsis())))
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
	if bar := s.p.liveBar(); bar != nil && IsTerminal(s.w) {
		return bar.above(func() (int, error) { return s.w.Write(b) })
	}
	return s.w.Write(b)
}

// endLive ends every line still drawing before something else takes the terminal: the running
// step settles, and the download bar clears and stops for good.
func (p *Printer) endLive(done bool) {
	p.settle(done)
	if bar := p.liveBar(); bar != nil {
		bar.halt()
	}
}

// errLocked is stderr for a line written with the step lock held, which lands above a live bar.
func (p *Printer) errLocked() io.Writer {
	if bar := p.steps.bar; bar != nil {
		return aboveBar{bar, p.Stderr}
	}
	return p.Stderr
}

type aboveBar struct {
	bar *Progress
	w   io.Writer
}

func (a aboveBar) Write(b []byte) (int, error) {
	return a.bar.above(func() (int, error) { return a.w.Write(b) })
}

func (p *Printer) liveBar() *Progress {
	p.steps.mu.Lock()
	defer p.steps.mu.Unlock()
	return p.steps.bar
}

func (p *Printer) release(pr *Progress) {
	p.steps.mu.Lock()
	defer p.steps.mu.Unlock()
	if p.steps.bar == pr {
		p.steps.bar = nil
	}
}
