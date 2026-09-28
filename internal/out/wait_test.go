package out

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// fakeChecks answers a wait's checks from a script, one answer per check, repeating the last.
type fakeChecks struct {
	answers [][]bool
	calls   int
}

func (f *fakeChecks) check() ([]WaitFile, error) {
	found := f.answers[min(f.calls, len(f.answers)-1)]
	f.calls++
	files := make([]WaitFile, len(found))
	for i, ok := range found {
		files[i] = WaitFile{Found: ok}
	}
	return files, nil
}

func newTestWait(f *fakeChecks) *waiter {
	return newWaiter(Theme{ASCII: true}, DownloadWait{
		Title: "2 files need a manual download into downloads",
		Files: []WaitFile{{Name: "a.jar", Page: "https://a"}, {Name: "b.jar", Page: "https://b"}},
		Check: f.check,
		Every: time.Millisecond,
	})
}

// run feeds msg to m and then every message its commands produce, leaving ticks unsent.
func run(m *waiter, msg tea.Msg) {
	queue := []tea.Msg{msg}
	for len(queue) > 0 {
		_, cmd := m.Update(queue[0])
		queue = append(queue[1:], msgs(cmd)...)
	}
}

func msgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		var all []tea.Msg
		for _, c := range msg {
			all = append(all, msgs(c)...)
		}
		return all
	case waitTickMsg, tea.QuitMsg, nil:
		return nil
	default:
		return []tea.Msg{msg}
	}
}

func TestWaitListsEachFileAsMissingOrFound(t *testing.T) {
	f := &fakeChecks{answers: [][]bool{{true, false}}}
	m := newTestWait(f)
	view := m.View()
	for _, want := range []string{"2 files need a manual download into downloads", "o a.jar", "https://a", "o b.jar", "Enter"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view lacks %q:\n%s", want, view)
		}
	}
	run(m, tea.KeyMsg{Type: tea.KeyEnter})
	if view := m.View(); !strings.Contains(view, "* a.jar") || !strings.Contains(view, "o b.jar") || m.done {
		t.Fatalf("enter checks, and a.jar is found:\n%s", view)
	}
}

func TestWaitEndsByItselfOnceEveryFileIsFound(t *testing.T) {
	f := &fakeChecks{answers: [][]bool{{true, false}, {true, true}}}
	m := newTestWait(f)
	run(m, waitTickMsg{})
	if m.done {
		t.Fatal("one file is still missing")
	}
	run(m, waitTickMsg{})
	if !m.done || m.skipped || m.err != nil {
		t.Fatalf("a tick finds the last file and the wait ends: done=%v skipped=%v err=%v", m.done, m.skipped, m.err)
	}
	if view := m.View(); strings.Contains(view, "Enter") || !strings.Contains(view, "* b.jar") {
		t.Fatalf("the last view is the checklist without its keys:\n%s", view)
	}
}

func TestWaitSkipsOnEscAndAbortsOnCtrlC(t *testing.T) {
	m := newTestWait(&fakeChecks{answers: [][]bool{{false, false}}})
	run(m, tea.KeyMsg{Type: tea.KeyEsc})
	if !m.done || !m.skipped {
		t.Fatalf("esc skips: done=%v skipped=%v", m.done, m.skipped)
	}
	m = newTestWait(&fakeChecks{answers: [][]bool{{false, false}}})
	run(m, tea.KeyMsg{Type: tea.KeyCtrlC})
	if !m.done || m.skipped || !errors.Is(m.err, ErrPickCancelled) {
		t.Fatalf("ctrl-c aborts: done=%v skipped=%v err=%v", m.done, m.skipped, m.err)
	}
}

func TestWaitChecksOnceAtATime(t *testing.T) {
	f := &fakeChecks{answers: [][]bool{{false, false}}}
	m := newTestWait(f)
	_, first := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, second := m.Update(waitTickMsg{})
	if first == nil {
		t.Fatal("enter starts a check")
	}
	for _, msg := range msgs(second) {
		if _, checked := msg.(waitCheckedMsg); checked {
			t.Fatal("a tick during a check starts no second one")
		}
	}
}
