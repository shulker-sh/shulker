package out

import (
	"regexp"
	"strings"
	"testing"
)

var sgrSeq = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func coloured() Theme { return Theme{Color: true, GreyIndex: GreyDark} }

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
		{Kind: Add, Name: "lithium", Version: "0.14.3", Targets: []string{"client"}, OfTargets: 2},
		{Kind: Add, Name: "ferritecore", Version: "6.0.2", Targets: []string{"client", "server"}, OfTargets: 2},
	}
	want := []string{
		"  + kitchen-sink 1.5 (pack)",
		"  + fabric-api   0.102.0+26.2 (required by lithium)",
		"  + lithium      0.14.3 » client",
		"  + ferritecore  6.0.2  » all targets",
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
	want := "  ✘ error: no shulker.lock; run shulker install after shulker init (lock-stale)"
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
	l := &Lines{T: Theme{}}
	pr := &Progress{l: l, verb: "fetching", sizes: map[string]int64{}}
	for _, f := range []Download{{"a.jar", 3 << 20}, {"b.jar", 1 << 20}} {
		pr.total++
		pr.longest = len(f.Name)
		pr.sizes[f.Name] = f.Size
		pr.totalBy += f.Size
	}
	pr.bytes = 2 << 20
	pr.current = "b.jar"
	got := pr.render(120)
	want := "  ⠋ fetching ━━━━━━━━━━╸───────── 0/2 (2.0 MB of 4.0 MB) b.jar"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := pr.render(60); len(got) != 2 || got[1] != "    b.jar (1.0 MB)" {
		t.Fatalf("narrow: got %q", got)
	}
	pr.totalBy = 0
	pr.done = 1
	if got := pr.render(120)[0]; got != "  ⠋ fetching ━━━━━━━━━━╸───────── 1/2 (2.0 MB) b.jar" {
		t.Fatalf("unknown sizes: got %q", got)
	}
}

func TestErrorRowsRenderStructured(t *testing.T) {
	e := &Error{Code: "validation-failed", Message: "1 problem(s) in the locked mods:\n  Problem 1\n    - ignored text", Items: []string{"ignored text"}}
	e.Rows = []Detail{{Text: "sodium 1.0 requires fabric-api >=2, not installed", Children: []Detail{
		{Text: "the ignore is stale"},
		{Label: "Fix", Text: "shulker add fabric-api", Command: true},
		{Label: "Ignore", Text: `{"rule":"depends"}`},
	}}}
	lines := render(Theme{}, func(l *Lines) { l.Error(e) })
	want := []string{
		"  ✘ error: 1 problem(s) in the locked mods (validation-failed)",
		"    └─ sodium 1.0 requires fabric-api >=2, not installed",
		"         the ignore is stale",
		"         Fix: shulker add fabric-api",
		`         Ignore: {"rule":"depends"}`,
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
	plain := render(Theme{}, func(l *Lines) { l.Error(&Error{Code: "x", Message: "top line\n  second\n\n  third"}) })
	if len(plain) != 3 || plain[1] != "    ├─ second" || plain[2] != "    └─ third" {
		t.Fatalf("remaining message lines should render plain: %q", plain)
	}
}

func TestPlainErrorDropsTerminalDecoration(t *testing.T) {
	e := &Error{
		Code:    "update-paused",
		Message: "This pack's update took longer than 4 minutes, so shulker paused it.",
		Plain:   true,
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
