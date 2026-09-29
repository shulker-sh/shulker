//go:build unix

package out

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

// ptyPrinter prints to both ends of a pseudo-terminal 80 columns wide and returns what reached
// the terminal so far, raw.
func ptyPrinter(t *testing.T) (*Printer, func() []byte) {
	master, tty, err := pty.Open()
	if err != nil {
		t.Skip("no pty:", err)
	}
	if err := pty.Setsize(master, &pty.Winsize{Rows: 24, Cols: 80}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var raw bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			mu.Lock()
			raw.Write(buf[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() { tty.Close(); master.Close(); <-done })
	// read waits for the terminal to go quiet for a few frames, or gives up after a while so a
	// line that never stops drawing still fails the test that reads it.
	read := func() []byte {
		var last []byte
		quiet := 0
		for deadline := time.Now().Add(5 * time.Second); quiet < 3 && time.Now().Before(deadline); {
			time.Sleep(frameEvery)
			mu.Lock()
			now := bytes.Clone(raw.Bytes())
			mu.Unlock()
			if bytes.Equal(now, last) {
				quiet++
			} else {
				quiet = 0
			}
			last = now
		}
		return last
	}
	return &Printer{Stdout: tty, Stderr: tty}, read
}

// screen replays raw terminal output onto a grid, following the moves a live line makes, and
// returns the rows with trailing blanks trimmed and the column the cursor ends in.
func screen(raw []byte, width int) (rows []string, col int) {
	var grid [][]rune
	row := 0
	put := func(r rune) {
		for len(grid) <= row {
			grid = append(grid, nil)
		}
		for len(grid[row]) <= col {
			grid[row] = append(grid[row], ' ')
		}
		grid[row][col] = r
		col++
		if col == width {
			row, col = row+1, 0
		}
	}
	s := []rune(string(raw))
	for i := 0; i < len(s); i++ {
		switch r := s[i]; {
		case r == '\r':
			col = 0
		case r == '\n':
			row++
		case r == '\x1b' && i+1 < len(s) && s[i+1] == '[':
			j := i + 2
			for j < len(s) && (s[j] < '@' || s[j] > '~') {
				j++
			}
			params, final := string(s[i+2:j]), s[j]
			i = j
			switch final {
			case 'A':
				n, _ := strconv.Atoi(params)
				row -= max(1, n)
			case 'J':
				if row < len(grid) {
					grid = grid[:row+1]
					grid[row] = grid[row][:min(col, len(grid[row]))]
				}
			}
		case r == '\x1b' && i+1 < len(s) && s[i+1] == ']':
			for i < len(s) && s[i] != '\a' && !(s[i] == '\\' && s[i-1] == '\x1b') {
				i++
			}
		case r >= ' ':
			put(r)
		}
	}
	for len(grid) <= row {
		grid = append(grid, nil)
	}
	for _, line := range grid[:row+1] {
		rows = append(rows, strings.TrimRight(string(line), " "))
	}
	return rows, col
}

func startBar(p *Printer) *Progress {
	pr := p.Progress("fetching", []Download{{Name: "a.jar", Size: 10}, {Name: "b.jar", Size: 10}})
	pr.File("a.jar")
	pr.Bytes(5)
	waitUntilDrawn(pr)
	return pr
}

func waitUntilDrawn(pr *Progress) {
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(frameEvery / 4) {
		pr.mu.Lock()
		drawn := len(pr.drawn) > 0
		pr.mu.Unlock()
		if drawn {
			return
		}
	}
}

func TestAnErrorClearsTheBarAndStartsAtColumnZero(t *testing.T) {
	p, read := ptyPrinter(t)
	pr := startBar(p)
	p.Fail(errors.New("the download broke"))
	pr.Advance()
	rows, col := screen(read(), 80)
	want := []string{"", "  ✘ the download broke (error)", "", ""}
	if strings.Join(rows, "\n") != strings.Join(want, "\n") || col != 0 {
		t.Fatalf("screen %q, cursor in column %d", rows, col)
	}
}

func TestALineDuringTheBarLandsAboveIt(t *testing.T) {
	p, read := ptyPrinter(t)
	pr := startBar(p)
	p.Warn("a.jar is slow")
	pr.Advance()
	pr.Advance()
	waitUntilDrawn(pr)
	pr.Finish()
	p.Finish()
	rows, col := screen(read(), 80)
	want := []string{"", "  ! a.jar is slow", "  ✔ Fetched 2 files (5 B)", "", ""}
	if strings.Join(rows, "\n") != strings.Join(want, "\n") || col != 0 {
		t.Fatalf("screen %q, cursor in column %d", rows, col)
	}
}

func TestAPromptEndsTheLiveLines(t *testing.T) {
	for name, start := range map[string]func(p *Printer){
		"step": func(p *Printer) { p.Step("resolving Minecraft 26.2") },
		"bar":  func(p *Printer) { startBar(p) },
	} {
		t.Run(name, func(t *testing.T) {
			p, read := ptyPrinter(t)
			start(p)
			p.openPrompt()
			before := read()
			if after := read(); !bytes.Equal(before, after) {
				t.Fatalf("still drawing after the prompt opened: %q", after[len(before):])
			}
			if _, col := screen(before, 80); col != 0 {
				t.Fatalf("prompt starts in column %d", col)
			}
		})
	}
}

func TestAPendingLineIsReplacedByWhatFollows(t *testing.T) {
	p, read := ptyPrinter(t)
	p.Err().OK("Launched pack as Notch (Minecraft 26.2)", "")
	p.Pending("Waiting for Minecraft to close")
	p.Out().OK("Minecraft closed after 12m 4s", "")
	p.Finish()
	rows, col := screen(read(), 80)
	want := []string{"", "  ✔ Launched pack as Notch (Minecraft 26.2)", "  ✔ Minecraft closed after 12m 4s", "", ""}
	if strings.Join(rows, "\n") != strings.Join(want, "\n") || col != 0 {
		t.Fatalf("screen %q, cursor in column %d", rows, col)
	}
}

func TestAPendingLineStaysOffATerminal(t *testing.T) {
	var stderr bytes.Buffer
	p := &Printer{Stdout: &bytes.Buffer{}, Stderr: &stderr}
	p.Pending("Waiting for Minecraft to close")
	p.Out().OK("Minecraft closed after 12m 4s", "")
	if stderr.String() != "  ○ Waiting for Minecraft to close\n" {
		t.Fatalf("stderr %q", stderr.String())
	}
}

func TestAStepDuringTheBarSettlesAboveIt(t *testing.T) {
	p, read := ptyPrinter(t)
	pr := startBar(p)
	p.Step("installing fabric 0.19.5")
	p.Step("copying config/ into the instance")
	pr.Advance()
	pr.Advance()
	pr.Finish()
	p.Finish()
	rows, col := screen(read(), 80)
	want := []string{"", "  ✔ Installed fabric 0.19.5", "  ✔ Copied config/ into the instance", "  ✔ Fetched 2 files (5 B)", "", ""}
	if strings.Join(rows, "\n") != strings.Join(want, "\n") || col != 0 {
		t.Fatalf("screen %q, cursor in column %d", rows, col)
	}
}

func TestASecondBarClearsTheFirst(t *testing.T) {
	p, read := ptyPrinter(t)
	first := startBar(p)
	second := p.Progress("fetching", []Download{{Name: "c.jar", Size: 10}})
	second.Advance()
	second.Finish()
	first.Advance()
	first.Finish()
	p.Finish()
	rows, col := screen(read(), 80)
	want := []string{"", "  ✔ Fetched 1 file", "  ✔ Fetched 1 file (5 B)", "", ""}
	if strings.Join(rows, "\n") != strings.Join(want, "\n") || col != 0 {
		t.Fatalf("screen %q, cursor in column %d", rows, col)
	}
}
