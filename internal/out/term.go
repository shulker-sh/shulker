package out

import (
	"math"
	"os"
	"regexp"
	"strconv"
	"time"

	"golang.org/x/term"
)

var (
	backgroundReply = regexp.MustCompile(`\x1b\]11;rgb:([0-9a-fA-F]+)/([0-9a-fA-F]+)/([0-9a-fA-F]+)`)
	attributesReply = regexp.MustCompile(`\x1b\[\?[0-9;]*c`)
)

// queryBackground asks the terminal for its background colour with OSC 11.
// DA1 follows as a sentinel: every terminal answers it, so its reply arriving
// first means OSC 11 is unsupported and there is no wait for the timeout.
func queryBackground(tty *os.File) ([3]float64, bool) {
	var rgb [3]float64
	in := os.Stdin
	fd := int(in.Fd())
	if !term.IsTerminal(fd) {
		return rgb, false
	}
	state, err := term.MakeRaw(fd)
	if err != nil {
		return rgb, false
	}
	defer term.Restore(fd, state)
	if err := in.SetReadDeadline(time.Now().Add(300 * time.Millisecond)); err != nil {
		return rgb, false
	}
	defer in.SetReadDeadline(time.Time{})
	if _, err := tty.WriteString("\x1b]11;?\x1b\\\x1b[c"); err != nil {
		return rgb, false
	}
	var buf []byte
	chunk := make([]byte, 256)
	for {
		n, err := in.Read(chunk)
		buf = append(buf, chunk[:n]...)
		if err != nil || attributesReply.Match(buf) {
			break
		}
	}
	m := backgroundReply.FindSubmatch(buf)
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

func terminalWidth(f *os.File) int {
	w, _, err := term.GetSize(int(f.Fd()))
	if err != nil || w <= 0 {
		return 80
	}
	return w
}
