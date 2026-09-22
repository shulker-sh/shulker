package out

import (
	"bytes"
	"encoding/json"
	"errors"
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
