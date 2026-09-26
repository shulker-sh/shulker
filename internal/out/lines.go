package out

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/charmbracelet/lipgloss/tree"
	"github.com/charmbracelet/x/ansi"
)

const gutter = "  "

// Lines writes human output to one stream in the house shape: the two-space gutter and the theme.
type Lines struct {
	W io.Writer
	T Theme
}

// Kind is the gutter glyph an Item carries.
type Kind int

const (
	Add Kind = iota
	Change
	Drop
	Note
	Good
)

// Item is one line in an aligned block: a gutter glyph, the bold subject,
// then either a version, a version change, or free text, and a grey aside.
type Item struct {
	Kind Kind
	Name string
	// Version prints grey after the name and aligns within the block.
	Version string
	// From and To print as a version change, the old grey and the new yellow.
	From, To string
	// Sides lists the sides the item lands on; OfSides is how many sides the
	// manifest declares. The list prints after » only when the item skips one
	// of them: grey "no sides" when it lands on none.
	Sides   []string
	OfSides int
	Text    string
	Aside   []string
}

func (l *Lines) line(s string) { fmt.Fprintln(l.W, gutter+Tilde(s)) }

func (l *Lines) Blank() { fmt.Fprintln(l.W) }

// Raw prints text with no gutter or styling, for values scripts read back.
func (l *Lines) Raw(text string) { fmt.Fprintln(l.W, text) }

// Diff prints a unified diff inside the gutter: the file header pair is
// dropped, hunk headers cyan, removed lines red, added lines green.
func (l *Lines) Diff(text string) {
	t := l.T
	for i, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if i < 2 && (strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ")) {
			continue
		}
		switch {
		case strings.HasPrefix(line, "@@"):
			line = t.Cyan(line)
		case strings.HasPrefix(line, "-"):
			line = t.Red(line)
		case strings.HasPrefix(line, "+"):
			line = t.Green(line)
		}
		fmt.Fprintln(l.W, gutter+gutter+line)
	}
}

func (l *Lines) Text(text string) { l.line(l.T.Markup(text)) }

func (l *Lines) Heading(text string) { l.line(l.T.Bold(text)) }

func (l *Lines) Items(items ...Item) {
	items = slices.Clone(items)
	for i := range items {
		items[i] = items[i].oneLine()
	}
	nameWidth, versionWidth := 0, 0
	for _, it := range items {
		if it.Version != "" || it.From != "" || it.Text != "" || it.skipsASide() {
			nameWidth = max(nameWidth, Width(it.Name))
		}
		if afterVersion(it) {
			versionWidth = max(versionWidth, Width(it.Version))
		}
	}
	for _, it := range items {
		l.line(l.item(it, nameWidth, versionWidth))
	}
}

func (l *Lines) item(it Item, nameWidth, versionWidth int) string {
	t := l.T
	var parts []string
	rest := []string{}
	if it.Version != "" {
		version := it.Version
		if afterVersion(it) {
			version = pad(version, versionWidth)
		}
		rest = append(rest, t.Grey(version))
	}
	if it.From != "" || it.To != "" {
		rest = append(rest, t.Bump(it.From, it.To))
	}
	if it.skipsASide() {
		rest = append(rest, t.Grey(t.ArrowInto())+" "+t.Sides(it.Sides))
	}
	if it.Text != "" {
		rest = append(rest, t.Markup(it.Text))
	}
	name := it.Name
	if len(rest) > 0 {
		name = pad(name, nameWidth)
	}
	parts = append(parts, l.glyph(it.Kind), t.Bold(name))
	parts = append(parts, rest...)
	return strings.Join(parts, " ") + t.Aside(strings.Join(it.Aside, ", "))
}

func (it Item) oneLine() Item {
	it.Name, it.Version, it.From, it.To, it.Text = OneLine(it.Name), OneLine(it.Version), OneLine(it.From), OneLine(it.To), OneLine(it.Text)
	aside := make([]string, len(it.Aside))
	for i, a := range it.Aside {
		aside[i] = OneLine(a)
	}
	it.Aside = aside
	return it
}

// OneLine fits text shulker doesn't write itself, such as a provider's display name, onto one
// line: each line break and the space around it becomes a single space, and the ends are trimmed.
func OneLine(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return strings.TrimSpace(s)
	}
	var parts []string
	for part := range strings.FieldsFuncSeq(s, func(r rune) bool { return r == '\r' || r == '\n' }) {
		if part = strings.TrimSpace(part); part != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, " ")
}

func afterVersion(it Item) bool {
	return it.From != "" || it.To != "" || it.skipsASide() || it.Text != ""
}

func (it Item) skipsASide() bool { return it.OfSides > 0 && len(it.Sides) < it.OfSides }

func (l *Lines) glyph(k Kind) string {
	t := l.T
	switch k {
	case Add:
		return t.paint("+", sgrGreen, sgrBold)
	case Change:
		return t.paint("~", sgrYellow, sgrBold)
	case Drop:
		return t.paint("-", sgrRed, sgrBold)
	case Good:
		return t.paint(t.GlyphOK(), sgrGreen, sgrBold)
	}
	return t.Grey(t.GlyphDot())
}

func pad(s string, width int) string {
	if n := width - Width(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

// Bump is a version change: the old version grey, the new one yellow.
func (t Theme) Bump(from, to string) string {
	return t.Grey(from) + " " + t.Grey(t.ArrowBump()) + " " + t.Yellow(to)
}

// Sides lists the sides something lands on.
func (t Theme) Sides(names []string) string {
	if len(names) == 0 {
		return t.Grey("no sides")
	}
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = t.Cyan(n)
	}
	return strings.Join(parts, t.Grey(", "))
}

func (l *Lines) OK(text string, aside string) {
	l.prose(l.T.paint(l.T.GlyphOK(), sgrGreen, sgrBold), l.T.Markup(text)+l.T.Aside(aside))
}

// OKInto is an ok line whose destination path is the first row of the tree under it, since a
// path never shares a line with prose; rows follow it.
func (l *Lines) OKInto(text, dest, aside string, rows ...Row) {
	t := l.T
	l.OK(text, aside)
	l.Tree(append([]Row{{Text: t.Link(t.Grey(dest), dest)}}, rows...)...)
}

func (l *Lines) Warn(text string) {
	l.prose(l.T.paint("!", sgrYellow, sgrBold), l.T.Markup(text))
}

// Info is the empty state or a passing remark: a cyan i and the message.
func (l *Lines) Info(text string) {
	l.prose(l.T.paint("i", sgrCyan, sgrBold), l.T.Markup(text))
}

// Muted is a whole line in grey, for progress notes that carry no result.
func (l *Lines) Muted(text string) { l.line(l.T.Grey(text)) }

// Plain is a line in the gutter exactly as given, with no backtick markup.
func (l *Lines) Plain(text string) { l.line(text) }

// Done is a finished step: the ok glyph and the text, all grey, so the result line stays the only green one.
func (l *Lines) Done(text string) { l.line(l.T.Grey(l.T.GlyphOK() + " " + text)) }

// Row is one row of a result's tree. An error's rows are Details instead.
type Row struct {
	Label    string
	Text     string
	Children []string
}

// Tree prints rows as branches under the line above, each child row nested one gutter further in.
func (l *Lines) Tree(rows ...Row) {
	if len(rows) == 0 {
		return
	}
	t := l.T
	root := tree.New().Enumerator(t.enumerator).Indenter(t.indenter).EnumeratorStyle(t.Style().Foreground(t.lipglossGrey()).PaddingLeft(2).PaddingRight(1))
	for _, row := range rows {
		node := tree.Root(l.rowText(row))
		for _, child := range row.Children {
			node.Child(child)
		}
		root.Child(node)
	}
	// The tree pads each line of a multi-line row to the row's width.
	for line := range strings.SplitSeq(root.String(), "\n") {
		l.line(strings.TrimRight(line, " "))
	}
}

func (l *Lines) rowText(row Row) string {
	t := l.T
	body := t.Markup(row.Text)
	if row.Label == "" {
		return body
	}
	head := t.Grey(row.Label + ":")
	if body == "" {
		return head
	}
	return head + " " + body
}

func (t Theme) enumerator(children tree.Children, i int) string {
	if i == children.Length()-1 {
		return t.GlyphElbowRound()
	}
	return t.GlyphTee()
}

func (t Theme) indenter(children tree.Children, i int) string {
	if i == children.Length()-1 {
		return "  "
	}
	return t.GlyphBar() + " "
}

// tableFloor is the narrowest a wrapped column goes; below it the table overflows instead.
const tableFloor = 12

// Table prints a header-rule table in the gutter: grey title-case headers over a grey rule, no
// other border, two spaces between columns, and style deciding each body cell's paint. It renders
// at its natural width; when that overflows the terminal less the gutter, only the widest column's
// cells wrap, at / and -, so the other columns keep their width and no header is ever clipped. The
// column wraps no narrower than tableFloor or its header, and past that the table overflows.
func (l *Lines) Table(headers []string, rows [][]string, style func(row, col int) lipgloss.Style) {
	t := l.T
	grey := t.Style().Foreground(t.lipglossGrey())
	cells := func(row, col int) lipgloss.Style {
		cell := grey
		if row != table.HeaderRow {
			cell = style(row, col)
		}
		if col == len(headers)-1 {
			return cell
		}
		return cell.PaddingRight(2)
	}
	render := func(rows [][]string) string {
		return table.New().Headers(headers...).Rows(rows...).
			Border(lipgloss.NormalBorder()).BorderStyle(grey).
			BorderTop(false).BorderBottom(false).BorderLeft(false).BorderRight(false).BorderColumn(false).
			StyleFunc(cells).Render()
	}
	shortened := make([][]string, len(rows))
	for i, row := range rows {
		shortened[i] = make([]string, len(row))
		for col, cell := range row {
			shortened[i][col] = Tilde(cell)
		}
	}
	rows = shortened
	rendered := render(rows)
	if excess := lipgloss.Width(rendered) - (TerminalWidth(l.W) - len(gutter)); excess > 0 {
		rendered = render(fold(headers, rows, excess))
	}
	for line := range strings.SplitSeq(rendered, "\n") {
		l.line(strings.TrimRight(line, " "))
	}
}

// Columns is a Table style that paints each column the same way on every row; a column past the
// list takes the last style given.
func Columns(styles ...lipgloss.Style) func(row, col int) lipgloss.Style {
	return func(_, col int) lipgloss.Style { return styles[min(col, len(styles)-1)] }
}

// fold wraps the cells of the widest column to take up the excess, down to that column's floor.
func fold(headers []string, rows [][]string, excess int) [][]string {
	widths := make([]int, len(headers))
	for _, row := range rows {
		for col, cell := range row {
			if col < len(widths) {
				widths[col] = max(widths[col], lipgloss.Width(cell))
			}
		}
	}
	widest := 0
	for col, w := range widths {
		if w > widths[widest] {
			widest = col
		}
	}
	limit := max(widths[widest]-excess, tableFloor, lipgloss.Width(headers[widest]))
	folded := make([][]string, len(rows))
	for i, row := range rows {
		folded[i] = slices.Clone(row)
		if widest < len(row) {
			folded[i][widest] = ansi.Wrap(row[widest], limit, "/-")
		}
	}
	return folded
}

// Nudge is a grey lead-in ending in a colon, then the command to run.
func (l *Lines) Nudge(lead, command string) {
	l.Blank()
	l.line(l.T.Grey(lead + ":"))
	l.line(gutter + l.T.Grey("$") + " " + l.T.Command(command))
}

// Error renders the error tree: the red line with its code, then the items,
// candidates, and help underneath.
func (l *Lines) Error(e *Error) {
	if e.IsPlain {
		l.plainError(e)
		return
	}
	t := l.T
	code := e.Code
	if code == "" {
		code = "error"
	}
	message, extra, _ := strings.Cut(e.Message, "\n")
	if !e.isNamed {
		message = Sentence(message)
	}
	l.prose(t.paint(t.GlyphError(), sgrRed, sgrBold), t.Bold(t.Markup(strings.TrimSuffix(message, ":")))+t.Aside(code))
	rows := l.detailRows(e, extra)
	if label, picks := e.picks(); len(picks) > 0 {
		var children []string
		for _, p := range picks {
			children = append(children, t.Grey(t.ArrowPick())+" "+t.Cyan(p.Show))
		}
		rows = append(rows, Row{Label: label, Children: children})
	}
	if e.Help != "" {
		rows = append(rows, Row{Label: "help", Text: Sentence(e.Help)})
	}
	l.Tree(rows...)
	if e.Nudge.Command != "" {
		l.Nudge(e.Nudge.Lead, e.Nudge.Command)
	}
}

// plainError renders an error as dialog body text. A launcher hook's stderr is shown to the player
// inside the launcher's own message, with no terminal around it, so the glyph, the code aside, the
// gutter and the nudge's prompt are all dropped and the command is indented instead.
func (l *Lines) plainError(e *Error) {
	message, extra, _ := strings.Cut(e.Message, "\n")
	l.Raw(message)
	for _, line := range append(strings.Split(extra, "\n"), e.Items...) {
		if line = strings.TrimSpace(line); line != "" {
			l.Raw(line)
		}
	}
	if e.Help != "" {
		l.Raw(e.Help)
	}
	if e.Nudge.Command != "" {
		l.Blank()
		l.Raw(e.Nudge.Lead + ":")
		l.Raw("    " + e.Nudge.Command)
	}
}

// detailRows paints what sits under the error line: the rows the site set,
// else the items, else the message's remaining lines, plain.
func (l *Lines) detailRows(e *Error, extra string) []Row {
	t := l.T
	var rows []Row
	switch {
	case len(e.Rows) > 0:
		for _, d := range e.Rows {
			row := Row{Label: d.Label, Text: d.Text}
			if d.Label == "" {
				row.Text = t.Grey(d.Text)
			}
			for _, c := range d.Children {
				switch {
				case c.Label == "":
					row.Children = append(row.Children, t.Grey(c.Text))
				case c.IsCommand:
					row.Children = append(row.Children, t.Grey(c.Label+":")+" "+t.Command(c.Text))
				default:
					row.Children = append(row.Children, t.Grey(c.Label+":")+" "+c.Text)
				}
			}
			rows = append(rows, row)
		}
	case len(e.Items) > 0:
		for _, item := range e.Items {
			rows = append(rows, Row{Text: t.Grey(item)})
		}
	default:
		for _, line := range strings.Split(extra, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				rows = append(rows, Row{Text: t.Grey(line)})
			}
		}
	}
	return rows
}

// Ago is how long before now a moment was, in the words shulker uses wherever it dates something:
// "just now", "5 minutes ago", "3 hours ago", "2 days ago".
func Ago(t time.Time) string { return Since(time.Since(t)) }

// Since is Ago for a duration already measured.
func Since(d time.Duration) string {
	plural := func(n int, unit string) string {
		if n == 1 {
			return "1 " + unit + " ago"
		}
		return fmt.Sprintf("%d %ss ago", n, unit)
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d/time.Minute), "minute")
	case d < 48*time.Hour:
		return plural(int(d/time.Hour), "hour")
	}
	return plural(int(d/(24*time.Hour)), "day")
}
