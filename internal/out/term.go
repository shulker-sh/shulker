package out

import (
	"io"
	"math"
	"os"
	"regexp"
	"strconv"

	"golang.org/x/term"
)

var (
	backgroundReply = regexp.MustCompile(`\x1b\]11;rgb:([0-9a-fA-F]+)/([0-9a-fA-F]+)/([0-9a-fA-F]+)`)
	attributesReply = regexp.MustCompile(`\x1b\[\?[0-9;]*c`)
)

func parseBackground(reply []byte) ([3]float64, bool) {
	var rgb [3]float64
	m := backgroundReply.FindSubmatch(reply)
	if m == nil {
		return rgb, false
	}
	for i := range 3 {
		v, err := strconv.ParseUint(string(m[i+1]), 16, 32)
		if err != nil {
			return rgb, false
		}
		rgb[i] = float64(v) / float64(uint64(1)<<(4*len(m[i+1]))-1)
	}
	return rgb, true
}

func luminance(rgb [3]float64) float64 {
	lin := func(v float64) float64 {
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(rgb[0]) + 0.7152*lin(rgb[1]) + 0.0722*lin(rgb[2])
}

// TerminalWidth is w's column count, or 80 when w isn't a terminal. A result stream settles the
// running step before each write, and that wrapper is looked through.
func TerminalWidth(w io.Writer) int {
	w = unwrap(w)
	if f, ok := w.(*os.File); ok && IsTerminal(w) {
		return terminalWidth(f)
	}
	return 80
}

// unwrap looks through the settling wrapper to the stream it writes to.
func unwrap(w io.Writer) io.Writer {
	if s, ok := w.(settling); ok {
		return s.w
	}
	return w
}

func terminalWidth(f *os.File) int {
	w, _, err := term.GetSize(int(f.Fd()))
	if err != nil || w <= 0 {
		return 80
	}
	return w
}

// width is how wide the prompts may draw: the terminal stderr is on, or the fallback width.
func (p *Printer) width() int {
	if f, ok := p.Stderr.(*os.File); ok {
		return terminalWidth(f)
	}
	return 80
}
