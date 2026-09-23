package game

import (
	"errors"
	"io"
	"os"
	"strings"
	"time"

	"shulker.sh/shulker/internal/instance"
)

// dumpTimeout is how long a game gets to finish printing its threads. HotSpot prints a dump in well
// under a second even for a modpack's hundreds of threads; one that never finishes is a JVM that
// ignores the request, like one started with -Xrs.
const dumpTimeout = 10 * time.Second

// ErrDumpTimeout is a game that did not print a whole thread dump in time.
var ErrDumpTimeout = errors.New("the game printed no thread dump")

const (
	dumpStart = "Full thread dump"
	// dumpEnd opens the last line of the thread list: "JNI global refs:" since JDK 10, "JNI global
	// references:" before it.
	dumpEnd = "JNI global ref"
)

// JcmdNotFoundError is a runtime with no jcmd beside its java, which is how Windows asks a JVM for a
// dump: it has no SIGQUIT, and the console ctrl-break that stands in for one needs the game's own
// console, which a detached game doesn't share.
type JcmdNotFoundError struct{ Path string }

func (e *JcmdNotFoundError) Error() string { return "no jcmd at " + e.Path }

// JcmdFailedError is a jcmd that ran and printed no dump, with what it said instead: the pid is no
// JVM, or one it may not attach to.
type JcmdFailedError struct{ Stderr string }

func (e *JcmdFailedError) Error() string { return "jcmd printed no thread dump: " + e.Stderr }

// Thread is one thread of a dump: its name and its stack as the JVM printed it, header line first.
type Thread struct {
	Name  string `json:"name"`
	Stack string `json:"stack"`
}

// Dump asks the game of an open run for a thread dump and hands back its text, which ends up in the
// run's log as well. The run's Java is where Windows finds jcmd.
func Dump(run instance.Launch) (string, error) { return dump(run, dumpTimeout) }

// Threads splits a dump into its threads, in the order the JVM printed them.
func Threads(dump string) []Thread {
	var threads []Thread
	for block := range strings.SplitSeq(strings.ReplaceAll(dump, "\r\n", "\n"), "\n\n") {
		block = strings.Trim(block, "\n")
		if !strings.HasPrefix(block, `"`) {
			continue
		}
		name, _, ok := strings.Cut(block[1:], `"`)
		if !ok {
			continue
		}
		threads = append(threads, Thread{Name: name, Stack: strings.TrimRight(block, " \t")})
	}
	return threads
}

// awaitDump waits for a whole dump to appear in the log past offset, which is how big the log was
// before the dump was asked for, so an older dump in the same log is never mistaken for this one.
func awaitDump(log string, offset int64, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		text, err := readFrom(log, offset)
		if err != nil {
			return "", err
		}
		if dump, ok := cutDump(text); ok {
			return dump, nil
		}
		if time.Now().After(deadline) {
			return "", ErrDumpTimeout
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// cutDump is the dump in text, from its heading through the line that ends its thread list.
func cutDump(text string) (string, bool) {
	start := strings.Index(text, dumpStart)
	if start < 0 {
		return "", false
	}
	rest := text[start:]
	end := strings.Index(rest, "\n"+dumpEnd)
	if end < 0 {
		return "", false
	}
	line := strings.IndexByte(rest[end+1:], '\n')
	if line < 0 {
		return "", false
	}
	return strings.TrimRight(rest[:end+1+line], "\r"), true
}

func readFrom(path string, offset int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return "", err
	}
	data, err := io.ReadAll(f)
	return string(data), err
}
