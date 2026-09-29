//go:build unix

package out

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

const typedAhead = "touch typed-ahead\n"

var exitedStatus = regexp.MustCompile(exitedMark + `(\d+)\r?\n`)

// TestTypeAheadSurvivesStartup types a command while a process starts, the way one is typed
// or pasted into the shell while the previous command runs, and checks it is still waiting for
// the shell once the process exits. Each probe in testdata/typeahead is its own module that
// starts on one Charm generation: v1 loses the line to Bubble Tea's init query, and v2 must not.
func TestTypeAheadSurvivesStartup(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the probes")
	}
	for _, c := range []struct {
		probe string
		keeps bool
	}{{"v1", false}, {"v2", true}} {
		t.Run(c.probe, func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "probe")
			build := exec.Command("go", "build", "-o", bin, ".")
			build.Dir = filepath.Join("testdata", "typeahead", c.probe)
			build.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
			if b, err := build.CombinedOutput(); err != nil {
				t.Fatalf("building the %s probe: %v\n%s", c.probe, err, b)
			}
			left := typeAhead(t, bin)
			if kept := left == typedAhead; kept != c.keeps {
				t.Errorf("%s: %q left waiting after startup, want kept = %v", c.probe, left, c.keeps)
			}
		})
	}
}

// typeAhead runs a program in a pty the way a shell does, in the foreground of the terminal's
// session, types a line as it starts, and returns what of the line is still waiting once it
// exits. The shell stays alive until then, since a session leader's exit flushes the terminal.
func typeAhead(t *testing.T, program ...string) string {
	t.Helper()
	master, tty, err := pty.Open()
	if err != nil {
		t.Skip("no pty:", err)
	}
	defer master.Close()
	defer tty.Close()
	if err := pty.Setsize(master, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
		t.Fatal(err)
	}
	shell := exec.Command("/bin/sh", append([]string{"-c", `"$@"; echo "` + exitedMark + `$?"; exec sleep 30`, "sh"}, program...)...)
	shell.Stdin, shell.Stdout, shell.Stderr = tty, tty, tty
	shell.Env = append(os.Environ(), "TERM=xterm-256color")
	shell.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	exited := make(chan string, 1)
	var answering sync.WaitGroup
	answering.Go(func() { answerQueries(master, exited) })
	if err := shell.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { shell.Process.Kill(); shell.Wait() }()
	if _, err := master.WriteString(typedAhead); err != nil {
		t.Fatal(err)
	}
	select {
	case status := <-exited:
		if status != "0" {
			t.Fatalf("%s exited %s", program[0], status)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("%s never exited", program[0])
	}
	left := waiting(t, tty)
	shell.Process.Kill()
	shell.Wait()
	// The master's read only ends once no end of the terminal is open.
	tty.Close()
	answering.Wait()
	return left
}

const exitedMark = "probe-exited:"

// answerQueries replies the way a terminal with a dark background does, so a query at startup
// finishes as quickly as it would in one, having read whatever was typed ahead of the reply. It
// sends the program's exit status once the shell prints it.
func answerQueries(master *os.File, exited chan<- string) {
	replies := map[string]string{
		"\x1b]11;?": "\x1b]11;rgb:1e1e/1e1e/1e1e\x1b\\",
		"\x1b[6n":   "\x1b[1;1R",
		"\x1b[c":    "\x1b[?62;22c",
	}
	var seen []byte
	buf := make([]byte, 4096)
	for {
		n, err := master.Read(buf)
		seen = append(seen, buf[:n]...)
		for query, reply := range replies {
			if i := bytes.Index(seen, []byte(query)); i >= 0 {
				master.WriteString(reply)
				seen = append(seen[:i:i], seen[i+len(query):]...)
			}
		}
		if m := exitedStatus.FindSubmatch(seen); m != nil {
			exited <- string(m[1])
			seen = nil
		}
		if err != nil {
			return
		}
	}
}

// waiting reads what the terminal still holds for the next program to read.
func waiting(t *testing.T, tty *os.File) string {
	fd := int(tty.Fd())
	var got []byte
	chunk := make([]byte, 256)
	for {
		var set unix.FdSet
		set.Set(fd)
		n, err := unix.Select(fd+1, &set, nil, nil, &unix.Timeval{Usec: 300_000})
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			return string(got)
		}
		read, err := unix.Read(fd, chunk)
		if read <= 0 || err != nil {
			return string(got)
		}
		got = append(got, chunk[:read]...)
	}
}
