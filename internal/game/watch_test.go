//go:build !windows

package game

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// TestMain doubles as the watcher this package's own test re-execs: a process started by Watch is
// this test binary, and the environment is what tells it which of the two it is meant to be.
func TestMain(m *testing.M) {
	body, watching := os.LookupEnv("GAME_TEST_WATCHER")
	if !watching {
		os.Exit(m.Run())
	}
	var req struct {
		Out string `json:"out"`
	}
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		os.Exit(2)
	}
	os.Stdout.WriteString(body + "\n")
	// Whatever asked for this has gone by now, which is the case the watcher lives in.
	time.Sleep(50 * time.Millisecond)
	os.WriteFile(req.Out, []byte("watched "+req.Out), 0o644)
	os.Exit(0)
}

func TestWatchHandsTheRequestOverStdinAndReadsOneLineBack(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	left := t.TempDir() + "/left"

	t.Setenv("GAME_TEST_WATCHER", `{"pid":4321}`)

	line, err := Watch(exe, nil, []byte(`{"out":`+quote(left)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(line)) != `{"pid":4321}` {
		t.Fatalf("the watcher answered %q", line)
	}

	// The watcher outlives this call: what it does after answering is the run it is there for.
	for i := 0; i < 400; i++ {
		if _, err := os.Stat(left); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the watcher stopped when the call that started it returned")
}

func TestWatchReportsAWatcherThatSaysNothing(t *testing.T) {
	if _, err := Watch(os.DevNull, nil, nil); err == nil {
		t.Fatal("a watcher that cannot even start is not a launch")
	}
	quiet := t.TempDir() + "/quiet"
	if err := os.WriteFile(quiet, []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Watch(quiet, nil, nil); err == nil {
		t.Fatal("a watcher that exits without answering is not a launch")
	}
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
