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
// Reading the reply would swallow anything typed ahead for the shell, so input
// already waiting skips the query.
func queryBackground(in, tty *os.File) ([3]float64, bool) {
	fd := int(in.Fd())
	if !term.IsTerminal(fd) {
		return [3]float64{}, false
	}
	state, err := term.MakeRaw(fd)
	if err != nil {
		return [3]float64{}, false
	}
	defer term.Restore(fd, state)
	// Raw mode makes a line still being typed readable too, so this sees it.
	if inputWaiting(fd) {
		return [3]float64{}, false
	}
	if _, err := tty.WriteString("\x1b]11;?\x1b\\\x1b[c"); err != nil {
		return [3]float64{}, false
	}
	return parseBackground(readReply(fd, replyTimeout))
}

func inputWaiting(fd int) bool {
	var set unix.FdSet
	set.Set(fd)
	n, err := unix.Select(fd+1, &set, nil, nil, &unix.Timeval{})
	return err != nil || n > 0
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
