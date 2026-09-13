package out

import (
	"fmt"
	"io"
	"regexp"
	"strings"
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
	// Targets lists where the item lands; OfTargets is how many targets exist.
	// The list prints after » when OfTargets is set: "all targets" when every
	// target, grey "no targets" when none.
	Targets   []string
	OfTargets int
	Text      string
	Aside     []string
}

func (l *Lines) line(s string) { fmt.Fprintln(l.W, gutter+s) }

func (l *Lines) Blank() { fmt.Fprintln(l.W) }

// Raw prints text with no gutter or styling, for values scripts read back.
func (l *Lines) Raw(text string) { fmt.Fprintln(l.W, text) }

func (l *Lines) Text(text string) { l.line(l.T.Markup(text)) }

func (l *Lines) Heading(text string) { l.line(l.T.Bold(text)) }

func (l *Lines) Items(items ...Item) {
	nameWidth, versionWidth := 0, 0
	for _, it := range items {
		if it.Version != "" || it.From != "" || it.Text != "" || it.OfTargets > 0 {
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
	if it.OfTargets > 0 {
		rest = append(rest, t.Grey(t.ArrowInto())+" "+t.Targets(it.Targets, it.OfTargets))
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
	return it.From != "" || it.To != "" || it.OfTargets > 0 || it.Text != ""
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
	return t.paint("i", sgrCyan, sgrBold)
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

func (t Theme) Targets(names []string, of int) string {
	switch {
	case len(names) == 0:
		return t.Grey("no targets")
	case len(names) >= of:
		return t.Cyan("all targets")
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

// Info is the empty state or a passing remark: a grey i and the message.
func (l *Lines) Info(text string) {
	l.line(l.T.paint("i", l.T.grey(), sgrBold) + " " + l.T.Markup(text))
}

// Muted is a whole line in grey, for progress notes that carry no result.
func (l *Lines) Muted(text string) { l.line(l.T.Grey(text)) }

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
	}
}

// Error renders the error tree: the red line with its code, then the items,
// candidates, and help underneath.
func (l *Lines) Error(e *Error) {
	t := l.T
	code := e.Code
	if code == "" {
		code = "error"
	}
	message, extra, _ := strings.Cut(e.Message, "\n")
	l.line(t.paint(t.GlyphError(), sgrRed, sgrBold) + " " + t.paint("error:", sgrRed, sgrBold) + " " + t.Bold(t.Markup(strings.TrimSuffix(message, ":"))) + t.Aside(code))
	rows, covered := l.messageRows(extra)
	for _, item := range e.Items {
		if !covered[item] {
			rows = append(rows, Row{Text: t.Grey(item)})
		}
	}
	if len(e.Candidates) > 0 {
		var picks []string
		for _, c := range e.Candidates {
			picks = append(picks, t.Grey(t.ArrowPick())+" "+t.Cyan(c))
		}
		rows = append(rows, Row{Label: "did you mean", Children: picks})
	}
	if e.Help != "" {
		rows = append(rows, Row{Label: "help", Text: e.Help})
	}
	l.Tree(rows...)
	if e.Nudge.Command != "" {
		l.Nudge(e.Nudge.Lead, e.Nudge.Command)
	}
}

var problemHeading = regexp.MustCompile(`^Problem \d+$`)

// messageRows turns the lines after an error's first line into tree rows:
// a line indented deeper than the one before nests under it, "Problem N"
// headings are dropped, and a leading "- " is the tree's job.
func (l *Lines) messageRows(extra string) ([]Row, map[string]bool) {
	t := l.T
	var rows []Row
	covered := map[string]bool{}
	indent := -1
	for _, raw := range strings.Split(extra, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || problemHeading.MatchString(line) {
			continue
		}
		depth := len(raw) - len(strings.TrimLeft(raw, " "))
		line = strings.TrimPrefix(line, "- ")
		if len(rows) > 0 && depth > indent {
			label, rest, labelled := strings.Cut(line, ": ")
			child := t.Grey(line)
			if labelled && !strings.Contains(label, " ") {
				child = t.Grey(label+":") + " " + rest
				if label == "Fix" {
					child = t.Grey(label+":") + " " + t.Command(rest)
				}
			}
			rows[len(rows)-1].Children = append(rows[len(rows)-1].Children, child)
			continue
		}
		indent = depth
		covered[line] = true
		rows = append(rows, Row{Text: t.Grey(line)})
	}
	return rows, covered
}
