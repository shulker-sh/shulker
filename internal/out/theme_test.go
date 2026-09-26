package out

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

var sgrSeq = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func coloured() Theme { return Theme{HasColor: true, GreyIndex: GreyDark} }

func render(t Theme, draw func(l *Lines)) []string {
	var b strings.Builder
	draw(&Lines{W: &b, T: t})
	return strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
}

func TestPaintReopensAfterNestedStyle(t *testing.T) {
	th := coloured()
	got := th.Bold(th.Markup("run `shulker install` after"))
	if !strings.Contains(got, sgrReset+sgrBold+" after") {
		t.Fatalf("bold must reopen after the command: %q", got)
	}
	if strings.HasSuffix(got, sgrBold+sgrReset) {
		t.Fatalf("no empty style before the final reset: %q", got)
	}
}

func TestItemsSpacing(t *testing.T) {
	items := []Item{
		{Kind: Add, Name: "kitchen-sink", Version: "1.5", Aside: []string{"pack"}},
		{Kind: Add, Name: "fabric-api", Version: "0.102.0+26.2", Aside: []string{"required by lithium"}},
		{Kind: Add, Name: "lithium", Version: "0.14.3", Sides: []string{"client"}, OfSides: 2},
		{Kind: Add, Name: "ferritecore", Version: "6.0.2", Sides: []string{"client", "server"}, OfSides: 2},
	}
	want := []string{
		"  + kitchen-sink 1.5 (pack)",
		"  + fabric-api   0.102.0+26.2 (required by lithium)",
		"  + lithium      0.14.3 » client",
		"  + ferritecore  6.0.2",
	}
	plain := render(Theme{}, func(l *Lines) { l.Items(items...) })
	colour := render(coloured(), func(l *Lines) { l.Items(items...) })
	for i := range want {
		if plain[i] != want[i] {
			t.Errorf("line %d:\n got %q\nwant %q", i, plain[i], want[i])
		}
		if got := sgrSeq.ReplaceAllString(colour[i], ""); got != want[i] {
			t.Errorf("line %d in colour:\n got %q\nwant %q", i, got, want[i])
		}
	}
}

func TestErrorLine(t *testing.T) {
	lines := render(coloured(), func(l *Lines) {
		l.Error(&Error{Code: "lock-stale", Message: "no shulker.lock; run `shulker install` after `shulker init`"})
	})
	want := "  ✘ No shulker.lock; run shulker install after shulker init (lock-stale)"
	if got := sgrSeq.ReplaceAllString(lines[0], ""); got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if !strings.Contains(lines[0], sgrReset+sgrBold+" after ") {
		t.Fatalf("the message must stay bold after an inline command: %q", lines[0])
	}
}

func TestBackToTopCountsRewrappedRows(t *testing.T) {
	if got, want := (&Progress{drawn: []int{100, 30}}).backToTop(60), "\x1b[3A\r\x1b[J"; got != want {
		t.Fatalf("a 100-column line re-wraps to two rows at 60: got %q, want %q", got, want)
	}
	if got, want := (&Progress{}).backToTop(60), "\r\x1b[J"; got != want {
		t.Fatalf("nothing drawn yet: got %q, want %q", got, want)
	}
}

func TestProgressFillsByBytesWhenSizesKnown(t *testing.T) {
	pr := newProgress(&Lines{T: Theme{}}, "fetching", []Download{{"a.jar", 3 << 20}, {"b.jar", 1 << 20}})
	pr.bytes = 2 << 20
	pr.current = "b.jar"
	if got := pr.fraction(); got != 0.5 {
		t.Fatalf("fraction by bytes = %v", got)
	}
	got := pr.render(120)
	want := "  ⣾ Fetching ───────────────   0% 0/2 (2.0 MB of 4.0 MB) b.jar"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := pr.render(60); len(got) != 2 || got[1] != "    b.jar (1.0 MB)" {
		t.Fatalf("narrow: got %q", got)
	}
	pr.totalBy = 0
	pr.done = 1
	if got := pr.fraction(); got != 0.5 {
		t.Fatalf("fraction by count = %v", got)
	}
	if got := pr.render(120)[0]; got != "  ⣾ Fetching ───────────────   0% 1/2 (2.0 MB) b.jar" {
		t.Fatalf("unknown sizes: got %q", got)
	}
}

func TestProgressBarEasesTowardTheCount(t *testing.T) {
	pr := newProgress(&Lines{T: Theme{ASCII: true}}, "fetching", []Download{{"a.jar", 0}, {"b.jar", 0}})
	pr.done = 2
	before := pr.bar.View()
	pr.ease()
	after := pr.bar.View()
	if !strings.HasPrefix(before, "-----") || strings.Contains(before, "#") {
		t.Fatalf("the bar starts empty: %q", before)
	}
	if !strings.HasPrefix(after, "#") || strings.HasSuffix(after, "100%") {
		t.Fatalf("one frame of easing fills some of the bar, not all: %q", after)
	}
	for range 100 {
		pr.ease()
	}
	if got := pr.bar.View(); got != "############### 100%" {
		t.Fatalf("settled bar %q", got)
	}
}

func TestErrorRowsRenderStructured(t *testing.T) {
	e := &Error{Code: "validation-failed", Message: "1 problem in the locked mods:\n  Problem 1\n    - ignored text", Items: []string{"ignored text"}}
	e.Rows = []Detail{{Text: "sodium 1.0 requires fabric-api >=2, not installed", Children: []Detail{
		{Text: "the ignore is stale"},
		{Label: "Fix", Text: "shulker add fabric-api", IsCommand: true},
		{Label: "Ignore", Text: `{"rule":"depends"}`},
	}}}
	lines := render(Theme{}, func(l *Lines) { l.Error(e) })
	want := []string{
		"  ✘ 1 problem in the locked mods (validation-failed)",
		"    ╰─ sodium 1.0 requires fabric-api >=2, not installed",
		"         ├─ the ignore is stale",
		"         ├─ Fix: shulker add fabric-api",
		`         ╰─ Ignore: {"rule":"depends"}`,
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	plain := render(Theme{}, func(l *Lines) { l.Error(&Error{Code: "x", Message: "top line\n  second\n\n  third"}) })
	if len(plain) != 3 || plain[1] != "    ├─ second" || plain[2] != "    ╰─ third" {
		t.Fatalf("remaining message lines should render plain: %q", plain)
	}
}

func TestPlainErrorDropsTerminalDecoration(t *testing.T) {
	e := &Error{
		Code:    "update-paused",
		Message: "This pack's update took longer than 4 minutes, so shulker paused it.",
		IsPlain: true,
		Nudge:   Nudge{Lead: "Launch again to resume it, or finish the download first with", Command: "shulker sync -i cozy"},
	}
	want := []string{
		"This pack's update took longer than 4 minutes, so shulker paused it.",
		"",
		"Launch again to resume it, or finish the download first with:",
		"    shulker sync -i cozy",
	}
	// Rendered with colour on, because a launcher dialog shows the bytes verbatim: no escapes, no
	// glyph, no code aside, no gutter, and no prompt in front of the command.
	got := render(coloured(), func(l *Lines) { l.Error(e) })
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
}

func TestDiffPaintsInsideTheGutter(t *testing.T) {
	lines := render(coloured(), func(l *Lines) {
		l.Diff("--- a/x\n+++ b/x\n@@ -1 +1 @@\n-a = 3\n+a = 2\n b\n")
	})
	want := []string{
		"    " + sgrCyan + "@@ -1 +1 @@" + sgrReset,
		"    " + sgrRed + "-a = 3" + sgrReset,
		"    " + sgrGreen + "+a = 2" + sgrReset,
		"     b",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got %q\nwant %q", lines, want)
	}
}

func TestTreeNestsChildrenUnderTheirRow(t *testing.T) {
	lines := render(Theme{}, func(l *Lines) {
		l.Tree(
			Row{Label: "loader", Text: "fabric 0.16.10"},
			Row{Label: "mods", Text: "3", Children: []string{"fabric-api", "sodium"}},
			Row{Label: "launcher", Text: "prism"},
		)
	})
	want := []string{
		"    ├─ loader: fabric 0.16.10",
		"    ├─ mods: 3",
		"    │    ├─ fabric-api",
		"    │    ╰─ sodium",
		"    ╰─ launcher: prism",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	ascii := render(Theme{ASCII: true}, func(l *Lines) { l.Tree(Row{Text: "a", Children: []string{"b"}}) })
	if strings.Join(ascii, "\n") != "    \\- a\n         \\- b" {
		t.Fatalf("ascii tree %q", ascii)
	}
	coloured := render(coloured(), func(l *Lines) { l.Tree(Row{Label: "help", Text: "run `shulker sync`"}) })
	if len(coloured) != 1 || !strings.Contains(coloured[0], sgrCyan+sgrBold+"shulker sync"+sgrReset) || !strings.HasPrefix(coloured[0], "    \x1b[38;5;248m╰─") {
		t.Fatalf("coloured tree %q", coloured)
	}
	if empty := render(Theme{}, func(l *Lines) { l.Tree() }); len(empty) != 1 || empty[0] != "" {
		t.Fatalf("an empty tree prints nothing: %q", empty)
	}
}

func TestTableRulesUnderGreyHeadersAndPadsCells(t *testing.T) {
	style := func(row, col int) lipgloss.Style { return lipgloss.NewStyle() }
	lines := render(Theme{}, func(l *Lines) {
		l.Table([]string{"", "Account", "UUID"}, [][]string{{"✔", "Steve", "8667ba71"}, {"", "Alt", "069a79f4"}}, style)
	})
	want := []string{
		"     Account  UUID",
		"  ────────────────────",
		"  ✔  Steve    8667ba71",
		"     Alt      069a79f4",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	coloured := render(coloured(), func(l *Lines) {
		l.Table([]string{"Name", "Note", "More"}, [][]string{{"x", "y", "z"}}, Columns(l.T.StyleBold(), l.T.StyleGrey()))
	})
	if !strings.HasPrefix(coloured[0], "  \x1b[38;5;248mName") || !strings.Contains(coloured[2], "\x1b[1mx\x1b[0m") || strings.Count(coloured[2], "\x1b[38;5;248m") != 2 {
		t.Fatalf("coloured table, columns past the list taking the last style: %q", coloured)
	}
}

func TestTableWrapsOnlyItsWidestColumn(t *testing.T) {
	style := func(row, col int) lipgloss.Style { return lipgloss.NewStyle() }
	path := "/Users/alex/Library/Application Support/PrismLauncher/instances/survival-1.21.4/minecraft"
	lines := render(Theme{}, func(l *Lines) {
		l.Table([]string{"Instance", "Path"}, [][]string{{"survival-1.21.4", path}, {"creative", "/tmp/creative"}}, style)
	})
	if len(lines) < 4 || lines[2] != "  survival-1.21.4  /Users/alex/Library/Application Support/PrismLauncher/" || !strings.HasPrefix(lines[3], "                   instances/") {
		t.Fatalf("the path folds at a separator and the instance column keeps its width:\n%s", strings.Join(lines, "\n"))
	}
	for _, line := range lines {
		if Width(line) > 80 {
			t.Fatalf("line over 80 columns: %q", line)
		}
	}
	two := render(Theme{}, func(l *Lines) {
		l.Table([]string{"Dir", "Message"}, [][]string{{strings.Repeat("a/", 30), strings.Repeat("word ", 20)}}, style)
	})
	if !strings.HasPrefix(two[2], "  "+strings.Repeat("a/", 30)+"  word word word") || Width(two[2]) > 80 {
		t.Fatalf("only the widest column folds; the other keeps its width:\n%s", strings.Join(two, "\n"))
	}
	floor := render(Theme{}, func(l *Lines) {
		l.Table([]string{"Dir", "Message"}, [][]string{{strings.Repeat("a/", 40), strings.Repeat("word ", 20)}}, style)
	})
	if !strings.HasPrefix(floor[2], "  "+strings.Repeat("a/", 40)+"  word word") || Width(floor[2]) <= 80 {
		t.Fatalf("at the floor the table overflows rather than folding every column:\n%s", strings.Join(floor, "\n"))
	}
}

func TestMarkdownRendersInTheThemeAndPrintsPlainWithoutColour(t *testing.T) {
	page := "### `shulker add`\n\nRun `shulker add sodium` to add [a mod](https://example.com).\n\n```sh\nshulker add sodium\n```\n\n| Flag | Description |\n| --- | --- |\n| `--pin` | Pin it |\n"
	if plain := render(Theme{}, func(l *Lines) { l.Markdown(page) }); strings.Join(plain, "\n") != strings.TrimSuffix(page, "\n") {
		t.Fatalf("without colour the markdown prints as it is:\n%s", strings.Join(plain, "\n"))
	}
	lines := render(coloured(), func(l *Lines) { l.Markdown(page) })
	text := strings.Join(lines, "\n")
	for _, want := range []string{"### shulker add", "shulker add sodium", "a mod", "https://example.com", "--pin", "Pin it"} {
		if !strings.Contains(sgrSeq.ReplaceAllString(text, ""), want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "`") || strings.Contains(text, "\x1b[34") || strings.Contains(text, "[4") {
		t.Fatalf("no backticks, no blue, no backgrounds:\n%q", text)
	}
	for _, line := range lines {
		if bare := sgrSeq.ReplaceAllString(line, ""); strings.TrimSpace(bare) != "" && !strings.HasPrefix(bare, gutter) {
			t.Fatalf("every line sits in the gutter: %q", line)
		}
	}
	for _, line := range lines {
		if strings.HasSuffix(line, " ") {
			t.Fatalf("no line carries glamour's padding: %q", line)
		}
	}
}
