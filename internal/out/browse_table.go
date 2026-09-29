package out

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// TableSource answers a table browser as its query changes. Rows runs off the drawing loop and may
// block; it answers for the query SetQuery last gave it.
type TableSource interface {
	SetQuery(query string)
	Rows() TableAnswer
}

// TableAnswer is a source's rows and status line. Query is the query the rows answer, which is an
// earlier one when the latest failed and the last good rows stay on screen. With no status, the
// status line counts the rows.
type TableAnswer struct {
	Query  string
	Rows   []TableRow
	Status string
}

// TableRow is one row's plain cells, which the browser styles by column, and what the row means.
type TableRow struct {
	Cells []string
	Value string
}

// TableBrowser is a query line over a table that follows it, with a details view per row.
type TableBrowser struct {
	Title   string
	Headers []string
	Styles  []lipgloss.Style
	// Cut is the column that gives up width when the table is wider than the terminal.
	Cut    int
	Source TableSource
	// Details is the details view of the row with value, for a terminal width wide. It runs off
	// the drawing loop and may make a request.
	Details func(value string, width int) string
	// Keys are the details view's own keys, besides esc.
	Keys []DetailKey
}

// DetailKey is a key the details view takes. One with Run does it in place; one without ends the
// browser with that key and the row's value.
type DetailKey struct {
	Key  string
	Help string
	Run  func(value string)
}

// BrowseTable draws b until it is left. Esc or ctrl-c leaves with an empty key, and a DetailKey
// with no Run leaves with its key and the value of the row whose details were open.
func (p *Printer) BrowseTable(b TableBrowser, in io.Reader) (key, value string, err error) {
	t := p.ErrTheme
	m := newTableBrowser(t, b, p.width())
	p.endLive(true)
	// The view draws the frame's opening line itself, so leaving with nothing clears it too; what
	// runs after opens the frame again when it prints.
	m.frame = p.opensFrame(p.Stderr)
	program := tea.NewProgram(m, tea.WithInput(in), tea.WithOutput(p.Stderr), tea.WithColorProfile(t.Profile()))
	if _, err := program.Run(); err != nil {
		return "", "", err
	}
	return m.key, m.value, nil
}

type browseFocus int

const (
	focusQuery browseFocus = iota
	focusResults
	focusDetails
)

type answerMsg struct {
	query  string
	answer TableAnswer
}

type detailsMsg struct {
	value string
	text  string
}

// tableBrowser keeps its rows, cursor and movement in a bubbles table and draws them itself: the
// table styles every column alike and counts colour codes as width. The window scrolls one row at
// a time, only once the cursor would leave it, and a new query's rows open at the top.
type tableBrowser struct {
	b     TableBrowser
	theme Theme
	width int
	frame bool
	input textinput.Model
	table table.Model
	focus browseFocus

	values    []string
	status    string
	rowsQuery string
	top       int

	detailsFor string
	details    string

	done       bool
	key, value string
}

func newTableBrowser(t Theme, b TableBrowser, width int) *tableBrowser {
	input := textinput.New()
	input.Prompt = "> "
	// The prompt and the text take no paint, and the cursor none beyond the block it draws.
	input.SetStyles(textinput.Styles{Cursor: textinput.CursorStyle{Shape: tea.CursorBlock, Blink: true}})
	input.Focus()
	columns := make([]table.Column, len(b.Headers))
	for i, h := range b.Headers {
		columns[i] = table.Column{Title: h, Width: Width(h)}
	}
	return &tableBrowser{
		b: b, theme: t, width: width, input: input,
		table: table.New(table.WithColumns(columns), table.WithHeight(pickRows)),
	}
}

func (m *tableBrowser) Init() tea.Cmd { return tea.Batch(textinput.Blink, m.ask()) }

// ask gives the source the query as it stands and asks for its rows off the loop.
func (m *tableBrowser) ask() tea.Cmd {
	query := m.input.Value()
	m.b.Source.SetQuery(query)
	return func() tea.Msg { return answerMsg{query: query, answer: m.b.Source.Rows()} }
}

func (m *tableBrowser) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case answerMsg:
		m.answered(msg)
	case detailsMsg:
		if m.focus == focusDetails && msg.value == m.detailsFor {
			m.details = msg.text
		}
	case tea.MouseWheelMsg:
		if m.focus != focusDetails {
			switch msg.Button {
			case tea.MouseWheelUp:
				m.move(-1)
			case tea.MouseWheelDown:
				m.move(1)
			}
		}
	case tea.PasteMsg:
		if m.focus == focusQuery {
			return m, m.edit(msg)
		}
	case tea.KeyPressMsg:
		return m.onKey(msg)
	}
	return m, nil
}

// edit hands a key or a paste to the query line, and asks again when it changed the query.
func (m *tableBrowser) edit(msg tea.Msg) tea.Cmd {
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		return tea.Batch(cmd, m.ask())
	}
	return cmd
}

// answered takes an answer to the query on the line; one to a query since typed past is dropped.
func (m *tableBrowser) answered(msg answerMsg) {
	if msg.query != m.input.Value() {
		return
	}
	m.status = msg.answer.Status
	rows := make([]table.Row, len(msg.answer.Rows))
	m.values = make([]string, len(msg.answer.Rows))
	for i, r := range msg.answer.Rows {
		rows[i] = table.Row(slices.Clone(r.Cells))
		m.values[i] = r.Value
	}
	m.table.SetRows(rows)
	if msg.answer.Query != m.rowsQuery {
		m.rowsQuery = msg.answer.Query
		m.table.GotoTop()
		m.top = 0
	}
	m.table.SetCursor(min(m.table.Cursor(), max(len(rows)-1, 0)))
	m.follow()
}

func (m *tableBrowser) onKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if k.String() == "ctrl+c" {
		return m.leave("", "")
	}
	switch m.focus {
	case focusQuery:
		switch k.String() {
		case "esc":
			return m.leave("", "")
		case "tab", "enter":
			if len(m.values) > 0 {
				m.focus = focusResults
				m.input.Blur()
			}
			return m, nil
		}
		return m, m.edit(k)
	case focusResults:
		switch k.String() {
		case "esc":
			return m.leave("", "")
		case "shift+tab":
			m.focus = focusQuery
			return m, m.input.Focus()
		case "enter":
			return m, m.open()
		}
		m.table.Focus()
		m.table, _ = m.table.Update(k)
		m.table.Blur()
		m.follow()
		return m, nil
	}
	if k.String() == "esc" {
		m.focus = focusResults
		return m, nil
	}
	for _, dk := range m.b.Keys {
		if k.String() != dk.Key {
			continue
		}
		if dk.Run == nil {
			return m.leave(dk.Key, m.detailsFor)
		}
		value := m.detailsFor
		return m, func() tea.Msg { dk.Run(value); return nil }
	}
	return m, nil
}

// open shows the details of the row under the cursor, which arrive off the loop.
func (m *tableBrowser) open() tea.Cmd {
	c := m.table.Cursor()
	if c < 0 || c >= len(m.values) {
		return nil
	}
	value, width := m.values[c], m.width
	m.focus, m.detailsFor, m.details = focusDetails, value, ""
	return func() tea.Msg { return detailsMsg{value: value, text: m.b.Details(value, width)} }
}

func (m *tableBrowser) leave(key, value string) (tea.Model, tea.Cmd) {
	m.done, m.key, m.value = true, key, value
	return m, tea.Quit
}

func (m *tableBrowser) move(by int) {
	if by < 0 {
		m.table.MoveUp(-by)
	} else {
		m.table.MoveDown(by)
	}
	m.follow()
}

// follow scrolls the window just far enough to keep the cursor in it.
func (m *tableBrowser) follow() {
	c := max(m.table.Cursor(), 0)
	if c < m.top {
		m.top = c
	}
	if c >= m.top+pickRows {
		m.top = c - pickRows + 1
	}
}

func (m *tableBrowser) View() tea.View {
	v := tea.NewView(m.content())
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m *tableBrowser) content() string {
	if m.done {
		return ""
	}
	t := m.theme
	var b strings.Builder
	if m.frame {
		b.WriteString("\n")
	}
	b.WriteString(gutter + t.Bold(m.b.Title) + "\n")
	if m.focus == focusDetails {
		details := m.details
		if details == "" {
			details = t.Grey("Loading" + t.Ellipsis())
		}
		b.WriteString("\n" + details + "\n\n")
		help := []string{"esc back"}
		for _, dk := range m.b.Keys {
			help = append(help, dk.Key+" "+dk.Help)
		}
		b.WriteString(gutter + t.Grey(strings.Join(help, " "+t.GlyphDot()+" ")))
		return b.String()
	}
	b.WriteString(gutter + m.input.View() + "\n\n")
	b.WriteString(gutter + m.statusLine() + "\n")
	b.WriteString(m.tableView())
	help := "tab results " + t.GlyphDot() + " esc exit"
	if m.focus == focusResults {
		help = "↑/↓ move " + t.GlyphDot() + " enter details " + t.GlyphDot() + " shift+tab query " + t.GlyphDot() + " esc exit"
	}
	b.WriteString("\n" + gutter + t.Grey(help))
	return b.String()
}

// statusLine is the source's status or, without one, the number of rows and, once they have the
// cursor, its position among them.
func (m *tableBrowser) statusLine() string {
	n := len(m.values)
	switch {
	case m.status != "" || n == 0:
		return m.status
	case n == 1:
		return "1 result"
	case m.focus == focusResults:
		return fmt.Sprintf("%d of %d results", m.table.Cursor()+1, n)
	}
	return fmt.Sprintf("%d results", n)
}

// tableView is the header, its rule and the window of rows, with a line above and below it
// counting the rows it hides that way. Widths are fitted over every row, not just the window, so
// columns hold still as it scrolls.
func (m *tableBrowser) tableView() string {
	t := m.theme
	rows := m.table.Rows()
	arrow := t.Cyan(t.ArrowPick()) + " "
	blank := strings.Repeat(" ", Width(t.ArrowPick())+1)
	var b strings.Builder
	if len(rows) == 0 {
		return ""
	}
	cells := make([][]string, len(rows))
	for i, r := range rows {
		cells[i] = []string(r)
	}
	widths, cut := fitColumns(m.b.Headers, cells, m.width-len(gutter)-Width(blank), m.b.Cut)
	line := func(row []string, style func(col int) lipgloss.Style) string {
		parts := make([]string, len(widths))
		for col, w := range widths {
			cell := ""
			if col < len(row) {
				cell = row[col]
			}
			if col == cut {
				cell = ansi.Truncate(cell, w, t.Ellipsis())
			}
			pad := strings.Repeat(" ", max(w-Width(cell), 0))
			parts[col] = style(col).Render(cell) + pad
		}
		return strings.TrimRight(strings.Join(parts, "  "), " ")
	}
	grey := t.StyleGrey()
	b.WriteString(gutter + blank + line(m.b.Headers, func(int) lipgloss.Style { return grey }) + "\n")
	total := 2 * (len(widths) - 1)
	for _, w := range widths {
		total += w
	}
	b.WriteString(gutter + blank + t.Grey(strings.Repeat("─", total)) + "\n")
	column := func(col int) lipgloss.Style { return m.b.Styles[min(col, len(m.b.Styles)-1)] }
	cursor := m.table.Cursor()
	end := min(m.top+pickRows, len(rows))
	if m.top > 0 {
		b.WriteString(gutter + blank + t.Grey(fmt.Sprintf("↑ %d more", m.top)) + "\n")
	}
	for i := m.top; i < end; i++ {
		mark := blank
		if i == cursor && m.focus == focusResults {
			mark = arrow
		}
		b.WriteString(gutter + mark + line(rows[i], column) + "\n")
	}
	if end < len(rows) {
		b.WriteString(gutter + blank + t.Grey(fmt.Sprintf("↓ %d more", len(rows)-end)) + "\n")
	}
	return b.String()
}
