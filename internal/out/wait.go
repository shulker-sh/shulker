package out

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// WaitFile is one row of a download checklist: the file's name over the page it comes from, and a
// note under them when something is wrong with a copy that was found.
type WaitFile struct {
	Name  string
	Page  string
	Found bool
	Note  string
}

// DownloadWait is a checklist of files to download by hand that ticks itself off.
type DownloadWait struct {
	Title string
	Files []WaitFile
	// Check looks for the files again and says where each stands, in Files' order. It runs off the
	// drawing loop, on enter and every Every.
	Check func() ([]WaitFile, error)
	Every time.Duration
	// Paste takes what was pasted or dragged into the terminal as paths to the files, and says
	// where each file stands after, with a note on what it couldn't take. It runs off the loop.
	Paste func(text string) ([]WaitFile, string, error)
}

// AwaitDownloads draws w until every file is found, or esc skips the rest, which reports skipped.
// Ctrl-c ends it with ErrPickCancelled, and a failed check with its error.
func (p *Printer) AwaitDownloads(ctx context.Context, w DownloadWait, in io.Reader) (skipped bool, err error) {
	m := newWaiter(p.ErrTheme, w)
	p.open(p.Stderr)
	if _, err := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(p.Stderr), tea.WithContext(ctx)).Run(); err != nil {
		if errors.Is(err, tea.ErrProgramKilled) {
			return false, ErrPickCancelled
		}
		return false, err
	}
	return m.skipped, m.err
}

type waitTickMsg struct{}

type waitCheckedMsg struct {
	files []WaitFile
	err   error
}

type waitPastedMsg struct {
	files []WaitFile
	note  string
	err   error
}

type waiter struct {
	theme    Theme
	w        DownloadWait
	checking bool
	note     string
	done     bool
	skipped  bool
	err      error
}

func newWaiter(t Theme, w DownloadWait) *waiter {
	return &waiter{theme: t, w: w}
}

func (m *waiter) Init() tea.Cmd { return tea.Batch(m.check(), m.tick()) }

func (m *waiter) tick() tea.Cmd {
	return tea.Tick(m.w.Every, func(time.Time) tea.Msg { return waitTickMsg{} })
}

func (m *waiter) check() tea.Cmd {
	if m.checking {
		return nil
	}
	m.checking = true
	return func() tea.Msg {
		files, err := m.w.Check()
		return waitCheckedMsg{files, err}
	}
}

func (m *waiter) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.done {
		return m, nil
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Paste && m.w.Paste != nil {
			text := string(msg.Runes)
			return m, func() tea.Msg {
				files, note, err := m.w.Paste(text)
				return waitPastedMsg{files, note, err}
			}
		}
		switch msg.Type {
		case tea.KeyCtrlC:
			return m.end(false, ErrPickCancelled)
		case tea.KeyEsc:
			return m.end(true, nil)
		case tea.KeyEnter:
			return m, m.check()
		}
	case waitTickMsg:
		return m, tea.Batch(m.check(), m.tick())
	case waitCheckedMsg:
		m.checking = false
		return m.update(msg.files, msg.err)
	case waitPastedMsg:
		m.note = msg.note
		return m.update(msg.files, msg.err)
	}
	return m, nil
}

func (m *waiter) update(files []WaitFile, err error) (tea.Model, tea.Cmd) {
	if err != nil {
		return m.end(false, err)
	}
	for i, f := range files {
		// A check that started before a paste took a file answers after it, without that file.
		m.w.Files[i].Found = m.w.Files[i].Found || f.Found
		if !m.w.Files[i].Found {
			m.w.Files[i].Note = f.Note
		}
	}
	if m.allFound() {
		return m.end(false, nil)
	}
	return m, nil
}

func (m *waiter) end(skipped bool, err error) (tea.Model, tea.Cmd) {
	m.done, m.skipped, m.err = true, skipped, err
	return m, tea.Quit
}

func (m *waiter) allFound() bool {
	for _, f := range m.w.Files {
		if !f.Found {
			return false
		}
	}
	return true
}

// View is the checklist, and while it waits the keys under it. The last view stays on screen as
// the record of what was found.
func (m *waiter) View() string {
	t := m.theme
	var b strings.Builder
	l := &Lines{W: &b, T: t}
	l.Warn(m.w.Title)
	for _, f := range m.w.Files {
		mark := t.Grey(t.GlyphPending())
		if f.Found {
			mark = t.paint(t.GlyphOK(), sgrGreen, sgrBold)
		}
		l.line("  " + mark + " " + f.Name)
		l.line("    " + t.Grey(f.Page))
		if f.Note != "" {
			l.line("    " + t.Yellow(f.Note))
		}
	}
	if m.done {
		return b.String()
	}
	if m.note != "" {
		l.Blank()
		l.Warn(m.note)
	}
	l.Blank()
	if m.w.Paste != nil {
		l.Muted("Press Enter to check now, drop a file here to take it, or Esc to skip the files still missing")
	} else {
		l.Muted("Press Enter to check now, or Esc to skip the files still missing")
	}
	return b.String()
}
