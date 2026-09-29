package out

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// keyPress is the key a terminal sends for name, as tea.KeyPressMsg's String spells it; any
// other name is typed text.
func keyPress(name string) tea.KeyPressMsg {
	named := map[string]tea.Key{
		"up":        {Code: tea.KeyUp},
		"down":      {Code: tea.KeyDown},
		"enter":     {Code: tea.KeyEnter},
		"esc":       {Code: tea.KeyEsc},
		"backspace": {Code: tea.KeyBackspace},
		"tab":       {Code: tea.KeyTab},
		"shift+tab": {Code: tea.KeyTab, Mod: tea.ModShift},
		"ctrl+c":    {Code: 'c', Mod: tea.ModCtrl},
	}
	if k, ok := named[name]; ok {
		return tea.KeyPressMsg(k)
	}
	return tea.KeyPressMsg{Code: []rune(name)[0], Text: name}
}

func press(m *picker, keys ...string) {
	for _, k := range keys {
		m.Update(keyPress(k))
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
	view := m.View().Content
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
	if view := m.View().Content; !strings.Contains(view, "    a\n") || !strings.Contains(view, "  * j\n") {
		t.Fatalf("the window moved before the cursor left it:\n%s", view)
	}
	press(m, "down")
	if view := m.View().Content; strings.Contains(view, "    a\n") || !strings.Contains(view, "    b\n") || !strings.Contains(view, "  * k\n") {
		t.Fatalf("the window didn't move by one row:\n%s", view)
	}
	press(m, "up", "up", "up", "up", "up", "up", "up", "up", "up")
	if view := m.View().Content; !strings.Contains(view, "  * b\n") || !strings.Contains(view, "    k\n") {
		t.Fatalf("the window moved while the cursor stayed inside it:\n%s", view)
	}
}

func TestPickerFiltersItsRows(t *testing.T) {
	m := newPicker(Theme{ASCII: true}, "Which?", pickChoices("26.2", "26.3", "1.21.1"))
	press(m, "/", "2", "6", ".", "3")
	if view := m.View().Content; strings.Contains(view, "26.2") || !strings.Contains(view, "  * 26.3\n") {
		t.Fatalf("filter left the wrong rows:\n%s", view)
	}
	press(m, "enter", "enter")
	if m.chosen() != "26.3" {
		t.Fatalf("chose %q", m.chosen())
	}
}

func TestPickerFilterTakesAPaste(t *testing.T) {
	m := newPicker(Theme{ASCII: true}, "Which?", pickChoices("26.2", "26.3", "1.21.1"))
	m.Update(tea.PasteMsg{Content: "26.2"})
	if m.query != "" {
		t.Fatalf("a paste outside the filter went into it: %q", m.query)
	}
	press(m, "/")
	m.Update(tea.PasteMsg{Content: "26.3"})
	if view := m.View().Content; m.query != "26.3" || !strings.Contains(view, "  * 26.3\n") || strings.Contains(view, "26.2") {
		t.Fatalf("pasted filter %q:\n%s", m.query, view)
	}
}

func TestPickerEscapeCancels(t *testing.T) {
	m := newPicker(Theme{ASCII: true}, "Sure?", pickChoices("no", "yes"))
	press(m, "esc")
	if !m.done || !m.cancelled {
		t.Fatal("esc didn't cancel")
	}
}
