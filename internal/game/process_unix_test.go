//go:build !windows

package game

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestStartRunsTheGameDetachedWithItsOutputInTheLog(t *testing.T) {
	dir := t.TempDir()
	script := fakeJava(t, dir, "pwd\necho \"$@\"\n")
	log := filepath.Join(dir, "game", ".shulker", "logs", "20260920-120000.log")

	g, err := Start(Launch{Java: script, Argv: []string{"-cp", "a.jar"}, Dir: dir, Log: log}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if g.PID == 0 {
		t.Fatal("a launch answers with the pid it started")
	}
	if code, err := g.Wait(); err != nil || code != 0 {
		t.Fatalf("status %d: %v", code, err)
	}

	body, err := os.ReadFile(log)
	if err != nil || !strings.Contains(string(body), "-cp a.jar") || !strings.Contains(string(body), filepath.Base(dir)) {
		t.Fatalf("the game's output and its directory should be in the log: %v\n%s", err, body)
	}
}

func TestWaitHandsBackTheStatusTheGameLeft(t *testing.T) {
	dir := t.TempDir()
	script := fakeJava(t, dir, "echo crashing\nexit 9\n")
	log := filepath.Join(dir, "run.log")

	g, err := Start(Launch{Java: script, Dir: dir, Log: log}, nil)
	if err != nil {
		t.Fatal(err)
	}
	code, err := g.Wait()
	if err != nil {
		t.Fatalf("a game that ran and failed is a status, not an error: %v", err)
	}
	if code != 9 {
		t.Fatalf("status %d", code)
	}
}

func TestStartMirrorsTheOutputToTheStreamAndStillWritesTheLog(t *testing.T) {
	dir := t.TempDir()
	script := fakeJava(t, dir, "echo to-both\n")
	log := filepath.Join(dir, "run.log")

	var mirror bytes.Buffer
	g, err := Start(Launch{Java: script, Dir: dir, Log: log}, &mirror)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.Wait(); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(log)
	if err != nil || !strings.Contains(string(body), "to-both") {
		t.Fatalf("the log keeps the output whether or not anyone is watching: %v\n%s", err, body)
	}
	if !strings.Contains(mirror.String(), "to-both") {
		t.Fatalf("the stream saw %q", mirror.String())
	}
}

func TestIsAliveTellsARunningProcessFromOneThatHasGone(t *testing.T) {
	dir := t.TempDir()
	script := fakeJava(t, dir, "sleep 1\n")
	log := filepath.Join(dir, "run.log")

	g, err := Start(Launch{Java: script, Dir: dir, Log: log}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !IsAlive(g.PID) {
		t.Fatal("a game that has not exited is alive")
	}
	if _, err := g.Wait(); err != nil {
		t.Fatal(err)
	}
	if IsAlive(g.PID) {
		t.Fatal("a game that has exited and been reaped is not")
	}
	if IsAlive(0) || IsAlive(-1) {
		t.Fatal("no process is no process")
	}
}

func fakeJava(t *testing.T, dir, body string) string {
	t.Helper()
	script := filepath.Join(dir, "java")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return script
}

// The game is in a session of its own, so a ctrl-c meant for the shell shulker was run from, or a
// watcher that was killed, leaves it running.
func TestStartPutsTheGameInAGroupOfItsOwn(t *testing.T) {
	dir := t.TempDir()
	g, err := Start(Launch{Java: fakeJava(t, dir, "sleep 1\n"), Dir: dir, Log: filepath.Join(dir, "run.log")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Wait()
	pgid, err := syscall.Getpgid(g.PID)
	if err != nil {
		t.Fatal(err)
	}
	if pgid != g.PID {
		t.Fatalf("the game's process group is %d, this process's is %d", pgid, syscall.Getpgrp())
	}
}
