package out

import (
	"bytes"
	"encoding/json"
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
