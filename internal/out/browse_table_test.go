package out

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/cursor"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type fakeTableSource struct {
	query   string
	rows    int
	details []string
}

func (s *fakeTableSource) SetQuery(query string) { s.query = query }

func (s *fakeTableSource) Rows() TableAnswer {
	var rows []TableRow
	for i := range s.rows {
		rows = append(rows, TableRow{Cells: []string{fmt.Sprintf("%s row %02d", s.query, i), "slug"}, Value: fmt.Sprint(i)})
	}
	return TableAnswer{Query: s.query, Rows: rows, Status: fmt.Sprintf("%d results", len(rows))}
}

func newTestBrowser(t *testing.T, rows int) (*tableBrowser, *fakeTableSource) {
	t.Helper()
	src := &fakeTableSource{rows: rows}
	m := newTableBrowser(Theme{}, TableBrowser{
		Title: "Search", Headers: []string{"Name", "Slug"}, Styles: []lipgloss.Style{lipgloss.NewStyle()},
		Source: src,
		Details: func(value string, _ int) string {
			src.details = append(src.details, value)
			return "details of " + value
		},
		Keys: []DetailKey{{Key: "a", Help: "add"}},
	}, 80)
	m.input.Cursor.SetMode(cursor.CursorStatic)
	return m, src
}

// typeQuery types text into the query line and delivers the answer its last change asked for.
func typeQuery(m *tableBrowser, text string) {
	var cmd tea.Cmd
	for _, r := range text {
		_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	deliver(m, cmd)
}

// deliver runs cmd and every command it batches, and hands their messages to m.
func deliver(m *tableBrowser, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			deliver(m, c)
		}
	case answerMsg, detailsMsg:
		m.Update(msg)
	}
}

func hit(m *tableBrowser, k tea.KeyType) tea.Cmd {
	_, cmd := m.Update(tea.KeyMsg{Type: k})
	return cmd
}

func wheel(m *tableBrowser, b tea.MouseButton) {
	m.Update(tea.MouseMsg{Button: b, Action: tea.MouseActionPress})
}

func TestTableBrowserOpensAtTheTop(t *testing.T) {
	m, _ := newTestBrowser(t, 30)
	typeQuery(m, "so")
	view := m.View()
	if !strings.Contains(view, "so row 00") || !strings.Contains(view, "so row 09") || strings.Contains(view, "so row 10") {
		t.Errorf("first window:\n%s", view)
	}
	if m.table.Cursor() != 0 || m.top != 0 {
		t.Errorf("cursor %d, top %d", m.table.Cursor(), m.top)
	}
}

func TestTableBrowserKeepsTheCursorInTheWindow(t *testing.T) {
	m, _ := newTestBrowser(t, 30)
	typeQuery(m, "so")
	hit(m, tea.KeyTab)
	for range 12 {
		hit(m, tea.KeyDown)
	}
	view := m.View()
	if m.top != 3 || !strings.Contains(view, "so row 12") || strings.Contains(view, "so row 02") {
		t.Errorf("past the bottom edge: top %d\n%s", m.top, view)
	}
	for range 11 {
		hit(m, tea.KeyUp)
	}
	if view := m.View(); m.top != 1 || !strings.Contains(view, "‣ so row 01") {
		t.Errorf("past the top edge: top %d\n%s", m.top, view)
	}
}

func TestTableBrowserResetsToTheTopForANewQuery(t *testing.T) {
	m, _ := newTestBrowser(t, 30)
	typeQuery(m, "so")
	hit(m, tea.KeyTab)
	for range 15 {
		hit(m, tea.KeyDown)
	}
	hit(m, tea.KeyShiftTab)
	typeQuery(m, "d")
	if view := m.View(); m.table.Cursor() != 0 || m.top != 0 || !strings.Contains(view, "sod row 00") {
		t.Errorf("new query: cursor %d, top %d\n%s", m.table.Cursor(), m.top, view)
	}
}

func TestTableBrowserScrollsWithTheWheel(t *testing.T) {
	m, _ := newTestBrowser(t, 30)
	typeQuery(m, "so")
	for range 11 {
		wheel(m, tea.MouseButtonWheelDown)
	}
	if m.table.Cursor() != 11 || m.top != 2 {
		t.Errorf("wheel down: cursor %d, top %d", m.table.Cursor(), m.top)
	}
	for range 3 {
		wheel(m, tea.MouseButtonWheelUp)
	}
	if m.table.Cursor() != 8 || m.top != 2 {
		t.Errorf("wheel up: cursor %d, top %d", m.table.Cursor(), m.top)
	}
}

func TestTableBrowserDropsAStaleAnswer(t *testing.T) {
	m, src := newTestBrowser(t, 3)
	typeQuery(m, "so")
	src.query = "old"
	m.Update(answerMsg{query: "s", answer: src.Rows()})
	if strings.Contains(m.View(), "old row") {
		t.Errorf("an answer to a query typed past was drawn:\n%s", m.View())
	}
}

func TestTableBrowserDetailsReturnInPlace(t *testing.T) {
	m, src := newTestBrowser(t, 30)
	typeQuery(m, "so")
	hit(m, tea.KeyEnter)
	hit(m, tea.KeyDown)
	hit(m, tea.KeyDown)
	deliver(m, hit(m, tea.KeyEnter))
	if len(src.details) != 1 || src.details[0] != "2" || !strings.Contains(m.View(), "details of 2") {
		t.Fatalf("details %v\n%s", src.details, m.View())
	}
	hit(m, tea.KeyEsc)
	if m.done || m.table.Cursor() != 2 || !strings.Contains(m.View(), "‣ so row 02") {
		t.Errorf("esc from details: done %v, cursor %d", m.done, m.table.Cursor())
	}
	deliver(m, hit(m, tea.KeyEnter))
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if !m.done || m.key != "a" || m.value != "2" || m.View() != "" {
		t.Errorf("a: done %v, key %q, value %q", m.done, m.key, m.value)
	}
}

func TestTableBrowserEscLeavesEmptyHanded(t *testing.T) {
	m, _ := newTestBrowser(t, 3)
	typeQuery(m, "so")
	hit(m, tea.KeyEsc)
	if !m.done || m.key != "" || m.value != "" || m.View() != "" {
		t.Errorf("esc: done %v, key %q, value %q, view %q", m.done, m.key, m.value, m.View())
	}
}

func TestTableBrowserCutsTheNameColumn(t *testing.T) {
	m, _ := newTestBrowser(t, 1)
	m.width = 30
	m.b.Cut = 0
	typeQuery(m, "a-very-long-query-that-wont-fit")
	if view := m.View(); !strings.Contains(view, "…") || !strings.Contains(view, "slug") {
		t.Errorf("narrow table:\n%s", view)
	}
}
