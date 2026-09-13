//go:build unix

package out

import (
	"errors"
	"os"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

const replyTimeout = 300 * time.Millisecond

// queryBackground asks the terminal for its background colour with OSC 11.
// DA1 follows as a sentinel: every terminal answers it, so its reply arriving
// first means OSC 11 is unsupported and there is no wait for the timeout.
func queryBackground(tty *os.File) ([3]float64, bool) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return [3]float64{}, false
	}
	state, err := term.MakeRaw(fd)
	if err != nil {
		return [3]float64{}, false
	}
	defer term.Restore(fd, state)
	if _, err := tty.WriteString("\x1b]11;?\x1b\\\x1b[c"); err != nil {
		return [3]float64{}, false
	}
	return parseBackground(readReply(fd, replyTimeout))
}

// readReply waits with select because Go's read deadlines are unsupported on
// a blocking terminal descriptor, which is what stdin usually is.
func readReply(fd int, timeout time.Duration) []byte {
	var buf []byte
	chunk := make([]byte, 256)
	deadline := time.Now().Add(timeout)
	for !attributesReply.Match(buf) {
		left := time.Until(deadline)
		if left <= 0 {
			break
		}
		var set unix.FdSet
		set.Set(fd)
		tv := unix.NsecToTimeval(left.Nanoseconds())
		n, err := unix.Select(fd+1, &set, nil, nil, &tv)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil || n == 0 {
			break
		}
		read, err := unix.Read(fd, chunk)
		if read > 0 {
			buf = append(buf, chunk[:read]...)
		}
		if err != nil || read <= 0 {
			break
		}
	}
	return buf
}
