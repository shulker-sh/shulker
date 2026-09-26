package out

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func press(m *picker, keys ...string) {
	for _, k := range keys {
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		switch k {
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		}
		m.Update(msg)
	}
}

func pickChoices(labels ...string) []Choice {
	choices := make([]Choice, len(labels))
	for i, l := range labels {
		choices[i] = Choice{Label: l, Value: l}
	}
	return choices
}

func TestPickerKeepsEveryRowInPlaceAsTheCursorMoves(t *testing.T) {
	m := newPicker(Theme{ASCII: true}, "Sure?", pickChoices("no", "yes"))
	press(m, "down")
	view := m.View()
	for _, want := range []string{"    no\n", "  * yes\n"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view lacks %q:\n%s", want, view)
		}
	}
	press(m, "enter")
	if !m.done || m.chosen() != "yes" {
		t.Fatalf("chose %q, done %v", m.chosen(), m.done)
	}
}

func TestPickerScrollsOnlyWhenTheCursorLeavesTheWindow(t *testing.T) {
	var labels []string
	for i := range 15 {
		labels = append(labels, string(rune('a'+i)))
	}
	m := newPicker(Theme{ASCII: true}, "Which?", pickChoices(labels...))
	press(m, "down", "down", "down", "down", "down", "down", "down", "down", "down")
	if view := m.View(); !strings.Contains(view, "    a\n") || !strings.Contains(view, "  * j\n") {
		t.Fatalf("the window moved before the cursor left it:\n%s", view)
	}
	press(m, "down")
	if view := m.View(); strings.Contains(view, "    a\n") || !strings.Contains(view, "    b\n") || !strings.Contains(view, "  * k\n") {
		t.Fatalf("the window didn't move by one row:\n%s", view)
	}
	press(m, "up", "up", "up", "up", "up", "up", "up", "up", "up")
	if view := m.View(); !strings.Contains(view, "  * b\n") || !strings.Contains(view, "    k\n") {
		t.Fatalf("the window moved while the cursor stayed inside it:\n%s", view)
	}
}

func TestPickerFiltersItsRows(t *testing.T) {
	m := newPicker(Theme{ASCII: true}, "Which?", pickChoices("26.2", "26.3", "1.21.1"))
	press(m, "/", "2", "6", ".", "3")
	if view := m.View(); strings.Contains(view, "26.2") || !strings.Contains(view, "  * 26.3\n") {
		t.Fatalf("filter left the wrong rows:\n%s", view)
	}
	press(m, "enter", "enter")
	if m.chosen() != "26.3" {
		t.Fatalf("chose %q", m.chosen())
	}
}

func TestPickerEscapeCancels(t *testing.T) {
	m := newPicker(Theme{ASCII: true}, "Sure?", pickChoices("no", "yes"))
	press(m, "esc")
	if !m.done || !m.cancelled {
		t.Fatal("esc didn't cancel")
	}
}
