//go:build unix

package out

import (
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

func blockingPipe(t *testing.T) (read, write int) {
	fds := make([]int, 2)
	if err := syscall.Pipe(fds); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { syscall.Close(fds[0]); syscall.Close(fds[1]) })
	return fds[0], fds[1]
}

func TestReadReplyOnBlockingDescriptor(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply string
		light bool
	}{
		{"dark", "\x1b]11;rgb:2828/2c2c/3434\x1b\\\x1b[?62;22c", false},
		{"light", "\x1b]11;rgb:fafa/fafa/fafa\x1b\\\x1b[?62;22c", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, w := blockingPipe(t)
			if _, err := syscall.Write(w, []byte(tc.reply)); err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			got := readReply(r, time.Second)
			if string(got) != tc.reply {
				t.Fatalf("got %q", got)
			}
			if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
				t.Fatalf("waited %v; the DA1 reply should end the wait", elapsed)
			}
			rgb, ok := parseBackground(got)
			if !ok || (luminance(rgb) > 0.4) != tc.light {
				t.Fatalf("parsed %v, ok %v", rgb, ok)
			}
		})
	}
}

func TestReadReplyGivesUpWhenTheTerminalIsSilent(t *testing.T) {
	r, _ := blockingPipe(t)
	start := time.Now()
	if got := readReply(r, 100*time.Millisecond); len(got) != 0 {
		t.Fatalf("got %q from a silent terminal", got)
	}
	if elapsed := time.Since(start); elapsed < 90*time.Millisecond || elapsed > time.Second {
		t.Fatalf("waited %v for a 100ms timeout", elapsed)
	}
}

func TestParseBackgroundWithoutOSC11(t *testing.T) {
	if _, ok := parseBackground([]byte("\x1b[?62;22c")); ok {
		t.Fatal("a DA1-only reply means OSC 11 is unsupported")
	}
}

func TestQueryLeavesTypedAheadInputForTheShell(t *testing.T) {
	master, tty, err := pty.Open()
	if err != nil {
		t.Skip("no pty:", err)
	}
	defer master.Close()
	defer tty.Close()
	if _, err := master.WriteString("shulker add modmenu\r"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	start := time.Now()
	if _, ok := queryBackground(tty, tty); ok {
		t.Fatal("queried the terminal over input already waiting")
	}
	if elapsed := time.Since(start); elapsed > replyTimeout/2 {
		t.Fatalf("waited %v on a query that should have been skipped", elapsed)
	}
	// The shell's line editor reads in raw mode, where input survives the query's mode switches.
	state, err := term.MakeRaw(int(tty.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	defer term.Restore(int(tty.Fd()), state)
	if got := readReply(int(tty.Fd()), 100*time.Millisecond); string(got) != "shulker add modmenu\n" {
		t.Fatalf("typed-ahead input after the query: %q", got)
	}
}
