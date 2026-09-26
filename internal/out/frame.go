package out

import (
	"fmt"
	"io"
	"sync"
)

// frame is the blank line above a run's first line and below its last, which sets the output
// apart from the prompt around it. It pads only a terminal, so a value a script reads back from
// a pipe arrives as printed.
type frame struct {
	mu     sync.Mutex
	opened bool
	last   io.Writer
}

// open writes the blank line above the output if w is the first terminal stream written to.
func (p *Printer) open(w io.Writer) {
	if s, ok := w.(settling); ok {
		w = s.w
	}
	if p.JSON || !p.isFramed(w) {
		return
	}
	p.frame.mu.Lock()
	defer p.frame.mu.Unlock()
	if !p.frame.opened {
		fmt.Fprintln(w)
		p.frame.opened = true
	}
	p.frame.last = w
}

// close writes the blank line below the output, on the stream written to last.
func (p *Printer) close() {
	p.frame.mu.Lock()
	defer p.frame.mu.Unlock()
	if p.frame.opened {
		fmt.Fprintln(p.frame.last)
		p.frame.opened = false
	}
}

func (p *Printer) isFramed(w io.Writer) bool {
	if p.Framed != nil {
		return p.Framed(w)
	}
	return IsTerminal(w)
}
