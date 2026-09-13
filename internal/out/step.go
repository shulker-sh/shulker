package out

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
	"time"
)

type stepState struct {
	mu      sync.Mutex
	running *step
	shown   []string
}

type step struct {
	text    string
	tty     *os.File
	stop    chan struct{}
	stopped chan struct{}
}

// Step shows a piece of work under way. On a terminal it spins until the next step or any other
// output, then settles into a grey ok line in the past tense; off a terminal only that line prints.
// A step already shown in this run is skipped.
func (p *Printer) Step(format string, args ...any) {
	if p.JSON {
		return
	}
	text := fmt.Sprintf(format, args...)
	p.steps.mu.Lock()
	defer p.steps.mu.Unlock()
	if slices.Contains(p.steps.shown, text) {
		return
	}
	p.steps.shown = append(p.steps.shown, text)
	p.settleLocked(true)
	if verb, _, _ := strings.Cut(text, " "); !strings.HasSuffix(verb, "ing") {
		(&Lines{W: p.Stderr, T: p.ErrTheme}).Done(text)
		return
	}
	s := &step{text: text}
	if f, ok := p.Stderr.(*os.File); ok && isTerminal(f) {
		s.tty = f
		s.stop, s.stopped = make(chan struct{}), make(chan struct{})
		go s.spin(p.ErrTheme)
	}
	p.steps.running = s
}

// Settle finishes the running step. Anything written through Out or Err settles first; code that
// writes to Stderr directly must call it.
func (p *Printer) Settle() { p.settle(true) }

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
		(&Lines{W: p.Stderr, T: p.ErrTheme}).Done(settledText(s.text))
	}
}

func (s *step) spin(t Theme) {
	defer close(s.stopped)
	tick := time.NewTicker(frameEvery)
	defer tick.Stop()
	for frame := 0; ; frame++ {
		spinner := spinnerFrames[frame%len(spinnerFrames)]
		if t.ASCII {
			spinner = string(spinnerASCII[frame%len(spinnerASCII)])
		}
		room := terminalWidth(s.tty) - len(gutter) - 3
		fmt.Fprint(s.tty, "\r\x1b[J"+gutter+t.paint(spinner, sgrCyan, sgrBold)+" "+t.Grey(clip(s.text, room, t.Ellipsis())))
		select {
		case <-s.stop:
			return
		case <-tick.C:
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
	return s.w.Write(b)
}
