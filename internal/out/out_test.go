package out

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestFailCarriesHelpIntoJSON(t *testing.T) {
	var stdout bytes.Buffer
	p := &Printer{JSON: true, Stdout: &stdout, Stderr: &bytes.Buffer{}}
	e := Errorf("lock-not-found", "no shulker.lock")
	e.Help = "run `shulker lock`"
	p.Fail(e)
	var env struct {
		Error struct {
			Help string `json:"help"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	if env.Error.Help != "run `shulker lock`" {
		t.Fatalf("help = %q in %s", env.Error.Help, stdout.String())
	}
}

func TestWithCauseAddsTheCauseAsARow(t *testing.T) {
	e := Errorf("store-incomplete", "can't read the version").WithCause("json", errors.New("unexpected end of JSON input"))
	if len(e.Rows) != 1 || e.Rows[0].Label != "json" || e.Rows[0].Text != "unexpected end of JSON input" {
		t.Fatalf("rows = %+v", e.Rows)
	}
	if e.Message != "can't read the version" {
		t.Fatalf("message = %q", e.Message)
	}
}

type recorded struct{ lines []string }

func (r *recorded) Warn(msg string)        { r.lines = append(r.lines, "warn "+msg) }
func (r *recorded) Error(code, msg string) { r.lines = append(r.lines, "error "+code+" "+msg) }
func (r *recorded) Result(data any)        { r.lines = append(r.lines, fmt.Sprintf("result %v", data)) }

func TestRecorderHearsEachWarningOnceAndEveryError(t *testing.T) {
	for _, json := range []bool{false, true} {
		r := &recorded{}
		p := &Printer{JSON: json, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Recorder: r}
		for range 3 {
			p.Warn("jei has no build for %s", "26.2")
		}
		p.WarnPrefix = "[friends] "
		p.Warn("jei has no build for %s", "26.2")
		p.Report(Errorf("sync-failed", "friends didn't sync"))
		p.Fail(Errorf("git-fetch", "can't fetch the pack"))
		want := []string{
			"warn jei has no build for 26.2",
			"warn [friends] jei has no build for 26.2",
			"error sync-failed friends didn't sync",
			"error git-fetch can't fetch the pack",
		}
		if !slices.Equal(r.lines, want) {
			t.Errorf("json %v: recorded %q", json, r.lines)
		}
	}
}

func TestRecorderHearsEachResultWhetherOrNotItPrintsAsJSON(t *testing.T) {
	for _, json := range []bool{false, true} {
		r := &recorded{}
		p := &Printer{JSON: json, Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Recorder: r}
		p.Step("fetching jei")
		p.Settle()
		if err := p.Emit(map[string]int{"wrote": 4}, func(l *Lines) { l.Info("wrote 4") }); err != nil {
			t.Fatal(err)
		}
		if want := []string{"result map[wrote:4]"}; !slices.Equal(r.lines, want) {
			t.Errorf("json %v: recorded %q", json, r.lines)
		}
	}
}

func TestReportPrintsOnlyForHumans(t *testing.T) {
	var human, machine bytes.Buffer
	(&Printer{Stdout: &bytes.Buffer{}, Stderr: &human}).Report(Errorf("sync-failed", "friends didn't sync"))
	(&Printer{JSON: true, Stdout: &machine, Stderr: &machine}).Report(Errorf("sync-failed", "friends didn't sync"))
	if !bytes.Contains(human.Bytes(), []byte("Friends didn't sync")) || machine.Len() != 0 {
		t.Fatalf("human %q, json %q", human.String(), machine.String())
	}
}

func TestWarnNudgePrintsTheNudgeUnderItsWarningOnce(t *testing.T) {
	var stderr bytes.Buffer
	p := &Printer{Stdout: &bytes.Buffer{}, Stderr: &stderr}
	for range 2 {
		p.WarnNudge(Nudge{Lead: "Update shulker", Command: "shulker self update"}, "%s is newer", "state.json")
	}
	want := gutter + "! state.json is newer\n\n" + gutter + "Update shulker:\n" + gutter + gutter + "$ shulker self update\n\n"
	if got := stderr.String(); got != want {
		t.Errorf("stderr = %q, want %q", got, want)
	}

	stderr.Reset()
	p = &Printer{JSON: true, Stdout: &bytes.Buffer{}, Stderr: &stderr}
	p.WarnNudge(Nudge{Lead: "Update shulker", Command: "shulker self update"}, "state.json is newer")
	if stderr.Len() != 0 || !slices.Equal(p.warnings, []string{"state.json is newer"}) {
		t.Errorf("json: stderr %q, warnings %q", stderr.String(), p.warnings)
	}
}

func TestAnnotationsEscapeWhatTheRunnerUnescapes(t *testing.T) {
	var stderr bytes.Buffer
	p := &Printer{Stdout: io.Discard, Stderr: &stderr, Annotate: true, JSON: true}
	p.Warn("100%% done\nnext")
	e := Errorf("some-code", "a: b, c:\nmore detail")
	e.Items = []string{"one: 1"}
	p.Fail(e)
	summary := Errorf("summary", "2 problems")
	summary.IsSummary = true
	p.Report(summary)
	want := "::warning::100%25 done%0Anext\n::error title=a%3A b%2C c (some-code)::one: 1\n"
	if stderr.String() != want {
		t.Fatalf("annotations:\n%q\nwant\n%q", stderr.String(), want)
	}
}

func TestItemsCollapseNewlinesInText(t *testing.T) {
	var buf bytes.Buffer
	l := &Lines{W: &buf}
	l.Items(
		Item{Kind: Note, Name: "\nRefurbished Furniture", Version: "1.0.22\n", Aside: []string{"client\nonly"}},
		Item{Kind: Change, Name: "jei", From: "19.57\n", To: "\n19.58"},
	)
	got := buf.String()
	if strings.Count(got, "\n") != 2 || !strings.Contains(got, "Refurbished Furniture 1.0.22") || !strings.Contains(got, "client only") || !strings.Contains(got, "19.57 ⟶ 19.58") {
		t.Fatalf("items: %q", got)
	}
}

func TestFinishPrintsAnEnvelopeWhenNothingWasEmitted(t *testing.T) {
	var stdout bytes.Buffer
	p := &Printer{JSON: true, Command: "hook pre-launch", Stdout: &stdout, Stderr: &bytes.Buffer{}}
	p.Warn("not an instance")
	p.Finish()
	var env Envelope
	if err := json.Unmarshal(stdout.Bytes(), &env); err != nil {
		t.Fatalf("%v: %q", err, stdout.String())
	}
	if !env.OK || env.Command != "hook pre-launch" || env.Data != nil || !slices.Equal(env.Warnings, []string{"not an instance"}) {
		t.Fatalf("envelope %+v", env)
	}
}

func TestFinishPrintsNothingMoreAfterEmitOrRaw(t *testing.T) {
	for name, run := range map[string]func(p *Printer){
		"emit": func(p *Printer) { _ = p.Emit(1, nil) },
		"raw":  func(p *Printer) { p.Raw() },
	} {
		var stdout bytes.Buffer
		p := &Printer{JSON: true, Stdout: &stdout, Stderr: &bytes.Buffer{}}
		run(p)
		before := stdout.Len()
		p.Finish()
		if stdout.Len() != before {
			t.Fatalf("%s: Finish printed %q", name, stdout.String()[before:])
		}
	}
}

func TestOutputIsFramedByBlankLinesOnATerminal(t *testing.T) {
	var term, pipe bytes.Buffer
	p := &Printer{Stdout: &pipe, Stderr: &term, Framed: func(w io.Writer) bool { return w == &term }}
	p.Step("fetched the pack")
	p.Warn("no build for %s", "jei")
	_ = p.Emit(nil, func(l *Lines) { l.Raw("value") })
	p.Finish()
	if got := term.String(); !strings.HasPrefix(got, "\n  ") || !strings.HasSuffix(got, "No build for jei\n\n") {
		t.Fatalf("the terminal isn't framed: %q", got)
	}
	if got := pipe.String(); got != "value\n" {
		t.Fatalf("a pipe got %q", got)
	}
}

func TestAFailedRunIsFramedToo(t *testing.T) {
	var term bytes.Buffer
	p := &Printer{Stdout: &term, Stderr: &term, Framed: func(io.Writer) bool { return true }}
	p.Fail(Errorf("lock-not-found", "no shulker.lock"))
	if got := term.String(); !strings.HasPrefix(got, "\n  ") || !strings.HasSuffix(got, "\n\n") || strings.HasSuffix(got, "\n\n\n") {
		t.Fatalf("got %q", got)
	}
}

func TestSentenceCapitalizesProseButNotNames(t *testing.T) {
	for text, want := range map[string]string{
		"couldn't clone the pack":        "Couldn't clone the pack",
		"shulker.json declares both":     "shulker.json declares both",
		"config_manager: side taken":     "config_manager: side taken",
		"fabric-api is missing":          "fabric-api is missing",
		"entityculling: taken from x":    "entityculling: taken from x",
		"`shulker lock` rewrites it":     "`shulker lock` rewrites it",
		"NeoForge has no release for 26": "NeoForge has no release for 26",
	} {
		if got := Sentence(text); got != want {
			t.Errorf("Sentence(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestAnErrorOpeningWithANameKeepsItsCase(t *testing.T) {
	var human bytes.Buffer
	(&Printer{Stdout: &bytes.Buffer{}, Stderr: &human}).Report(Errorf("mod-not-found", "%s isn't on Modrinth", "sodium"))
	if !strings.Contains(human.String(), "✘ sodium isn't on Modrinth") {
		t.Fatalf("got %q", human.String())
	}
}

func TestTildeShortensHomePathsButNotLinkTargets(t *testing.T) {
	dir := home()
	if dir == "" {
		t.Skip("no home directory")
	}
	sep := string(filepath.Separator)
	if got := Tilde("synced into " + filepath.Join(dir, "x", "y")); got != "synced into ~"+sep+filepath.Join("x", "y") {
		t.Fatalf("got %q", got)
	}
	if got := Tilde(dir + "-other"); got != dir+"-other" {
		t.Fatalf("a sibling of home changed: %q", got)
	}
	path := filepath.Join(dir, "pack")
	linked := Theme{HasColor: true, HasLinks: true}.Link(path, path)
	if got := Tilde(linked); !strings.Contains(got, "file://") || !strings.Contains(got, filepath.ToSlash(path)+"\x1b\\") || !strings.Contains(got, "\x1b\\~"+sep+"pack") {
		t.Fatalf("got %q", got)
	}
}

func TestWrapProseKeepsPathsWhole(t *testing.T) {
	text := "Couldn't back up the worlds in ~/Library/Application Support/PrismLauncher/instances/pack before the mods changed"
	lines := wrapProse(text, 40)
	want := []string{"Couldn't back up the worlds in", "~/Library/Application Support/PrismLauncher/instances/pack", "before the mods changed"}
	if !slices.Equal(lines, want) {
		t.Fatalf("got %q", lines)
	}
	if got := wrapProse(text, 0); len(got) != 1 {
		t.Fatalf("a limit of 0 wraps: %q", got)
	}
}

func TestWarningLinesAfterTheFirstAreTreeRows(t *testing.T) {
	var stderr bytes.Buffer
	p := &Printer{Stdout: io.Discard, Stderr: &stderr}
	p.Warn("dropped 2 defaults\n%s\n%s", "options.txt", "config/a.toml")
	want := "  ! Dropped 2 defaults\n    ├─ options.txt\n    ╰─ config/a.toml\n"
	if stderr.String() != want {
		t.Fatalf("warning:\n%q\nwant\n%q", stderr.String(), want)
	}
}
