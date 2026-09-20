package out

import (
	"fmt"
	"io"
	"strings"
	"time"
)

const gutter = "  "

type Lines struct {
	W io.Writer
	T Theme
}

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
	// manifest declares. The list prints after » when OfSides is set: "all
	// sides" when every side, grey "no sides" when none.
	Sides   []string
	OfSides int
	Text    string
	Aside   []string
}

func (l *Lines) line(s string) { fmt.Fprintln(l.W, gutter+s) }

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
	nameWidth, versionWidth := 0, 0
	for _, it := range items {
		if it.Version != "" || it.From != "" || it.Text != "" || it.OfSides > 0 {
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
	if it.OfSides > 0 {
		rest = append(rest, t.Grey(t.ArrowInto())+" "+t.Sides(it.Sides, it.OfSides))
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

func afterVersion(it Item) bool {
	return it.From != "" || it.To != "" || it.OfSides > 0 || it.Text != ""
}

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

func (t Theme) Bump(from, to string) string {
	return t.Grey(from) + " " + t.Grey(t.ArrowBump()) + " " + t.Yellow(to)
}

func (t Theme) Sides(names []string, of int) string {
	switch {
	case len(names) == 0:
		return t.Grey("no sides")
	case len(names) >= of:
		return t.Cyan("all sides")
	}
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = t.Cyan(n)
	}
	return strings.Join(parts, t.Grey(", "))
}

func (l *Lines) OK(text string, aside string) {
	l.line(l.T.paint(l.T.GlyphOK(), sgrGreen, sgrBold) + " " + l.T.Markup(text) + l.T.Aside(aside))
}

// OKInto is an ok line with a destination path after ».
func (l *Lines) OKInto(text, dest, aside string) {
	t := l.T
	l.line(t.paint(t.GlyphOK(), sgrGreen, sgrBold) + " " + t.Markup(text) + " " + t.Grey(t.ArrowInto()) + " " + t.Link(t.Grey(dest), dest) + t.Aside(aside))
}

func (l *Lines) Warn(text string) {
	l.line(l.T.paint("!", sgrYellow, sgrBold) + " " + l.T.Markup(text))
}

// Info is the empty state or a passing remark: a cyan i and the message.
func (l *Lines) Info(text string) {
	l.line(l.T.paint("i", sgrCyan, sgrBold) + " " + l.T.Markup(text))
}

// Muted is a whole line in grey, for progress notes that carry no result.
func (l *Lines) Muted(text string) { l.line(l.T.Grey(text)) }

// Plain is a line in the gutter exactly as given, with no backtick markup.
func (l *Lines) Plain(text string) { l.line(text) }

// Done is a finished step: the ok glyph and the text, all grey, so the result line stays the only green one.
func (l *Lines) Done(text string) { l.line(l.T.Grey(l.T.GlyphOK() + " " + text)) }

type Row struct {
	Label    string
	Text     string
	Children []string
}

func (l *Lines) Tree(rows ...Row) { l.tree(gutter+gutter, rows) }

func (l *Lines) tree(indent string, rows []Row) {
	t := l.T
	for i, row := range rows {
		last := i == len(rows)-1
		branch := t.GlyphTee()
		if last {
			branch = t.GlyphElbow()
		}
		body := t.Markup(row.Text)
		if row.Label != "" {
			head := t.Grey(row.Label + ":")
			if body != "" {
				body = head + " " + body
			} else {
				body = head
			}
		}
		fmt.Fprintln(l.W, indent+t.Grey(branch)+" "+body)
		below := t.Grey(t.GlyphBar())
		if last {
			below = " "
		}
		for _, child := range row.Children {
			fmt.Fprintln(l.W, indent+below+"    "+child)
		}
	}
}

// Nudge is a grey lead-in ending in a colon, then the command to run.
func (l *Lines) Nudge(lead, command string) {
	l.Blank()
	l.line(l.T.Grey(lead + ":"))
	l.line(gutter + l.T.Grey("$") + " " + l.T.Command(command))
}

type Entry struct {
	Synced bool
	Name   string
	Tag    string
	Aside  string
	Path   string
	Detail string
	// Note is a last grey line under the detail, for a problem the status aside is too narrow to
	// carry.
	Note string
}

// Entries is a bold heading with a tree of status-dotted entries, each with
// its full path on the line below.
func (l *Lines) Entries(heading string, entries []Entry) {
	t := l.T
	l.Heading(heading)
	for i, e := range entries {
		last := i == len(entries)-1
		branch, below := t.GlyphTee(), t.Grey(t.GlyphBar())
		if last {
			branch, below = t.GlyphElbow(), " "
		}
		dot := t.paint(t.GlyphDot(), sgrYellow, sgrBold)
		if e.Synced {
			dot = t.paint(t.GlyphDot(), sgrGreen, sgrBold)
		}
		line := t.Grey(branch) + " " + dot + " " + t.Bold(e.Name)
		if e.Tag != "" {
			line += " " + t.Cyan(e.Tag)
		}
		fmt.Fprintln(l.W, gutter+gutter+line+t.Aside(e.Aside))
		if e.Path != "" {
			fmt.Fprintln(l.W, gutter+gutter+below+"    "+t.Link(t.Grey(e.Path), e.Path))
		}
		if e.Detail != "" {
			fmt.Fprintln(l.W, gutter+gutter+below+"    "+t.Grey(e.Detail))
		}
		if e.Note != "" {
			fmt.Fprintln(l.W, gutter+gutter+below+"    "+t.Grey(e.Note))
		}
	}
}

// Error renders the error tree: the red line with its code, then the items,
// candidates, and help underneath.
func (l *Lines) Error(e *Error) {
	if e.Plain {
		l.plainError(e)
		return
	}
	t := l.T
	code := e.Code
	if code == "" {
		code = "error"
	}
	message, extra, _ := strings.Cut(e.Message, "\n")
	l.line(t.paint(t.GlyphError(), sgrRed, sgrBold) + " " + t.paint("error:", sgrRed, sgrBold) + " " + t.Bold(t.Markup(strings.TrimSuffix(message, ":"))) + t.Aside(code))
	rows := l.detailRows(e, extra)
	if label, picks := e.picks(); len(picks) > 0 {
		var children []string
		for _, p := range picks {
			children = append(children, t.Grey(t.ArrowPick())+" "+t.Cyan(p.Show))
		}
		rows = append(rows, Row{Label: label, Children: children})
	}
	if e.Help != "" {
		rows = append(rows, Row{Label: "help", Text: e.Help})
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
				case c.Command:
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
