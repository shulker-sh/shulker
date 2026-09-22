package out

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
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

func TestReportPrintsOnlyForHumans(t *testing.T) {
	var human, machine bytes.Buffer
	(&Printer{Stdout: &bytes.Buffer{}, Stderr: &human}).Report(Errorf("sync-failed", "friends didn't sync"))
	(&Printer{JSON: true, Stdout: &machine, Stderr: &machine}).Report(Errorf("sync-failed", "friends didn't sync"))
	if !bytes.Contains(human.Bytes(), []byte("friends didn't sync")) || machine.Len() != 0 {
		t.Fatalf("human %q, json %q", human.String(), machine.String())
	}
}
