package game

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// argsJava is a java that writes its argv to a file and exits with the status it is told to.
func argsJava(t *testing.T, exit string) (java, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args.txt")
	return fakeJava(t, dir, "printf '%s\\n' \"$@\" > \""+argsFile+"\"\nexit "+exit+"\n"), argsFile
}

func readArgs(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRunPassesTheGamesExitCodeBack(t *testing.T) {
	java, argsFile := argsJava(t, "3")

	code, gaveWay, err := Run(Launch{Java: java, Argv: []string{"--gameDir", "here"}}, nil, nil, nil)

	if err != nil || gaveWay != nil || code != 3 {
		t.Fatalf("code=%d gaveWay=%v err=%v", code, gaveWay, err)
	}
	if got := readArgs(t, argsFile); got != "--gameDir\nhere\n" {
		t.Fatalf("java got %q", got)
	}
}

func TestRunPrependsTheWrapper(t *testing.T) {
	java, argsFile := argsJava(t, "0")
	dir := t.TempDir()
	wrapper, wrapperArgs := filepath.Join(dir, "wrapper"), filepath.Join(dir, "wrapper-args.txt")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"" + wrapperArgs + "\"\nshift\nexec \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	code, gaveWay, err := Run(Launch{Java: java, Argv: []string{"--gameDir", "here"}, Wrapper: []string{wrapper, "--tag"}}, nil, nil, nil)

	if err != nil || gaveWay != nil || code != 0 {
		t.Fatalf("code=%d gaveWay=%v err=%v", code, gaveWay, err)
	}
	if got := readArgs(t, wrapperArgs); got != "--tag\n"+java+"\n--gameDir\nhere\n" {
		t.Fatalf("the wrapper should run first with Java after it, got %q", got)
	}
	if got := readArgs(t, argsFile); got != "--gameDir\nhere\n" {
		t.Fatalf("java should still get the argv, got %q", got)
	}
}

func TestRunFallsBackToJavaWhenTheWrapperCantRun(t *testing.T) {
	java, argsFile := argsJava(t, "0")

	code, gaveWay, err := Run(Launch{Java: java, Argv: []string{"--gameDir", "here"}, Wrapper: []string{"shulker-no-such-wrapper", "--tag"}}, nil, nil, nil)

	if err != nil || code != 0 {
		t.Fatalf("a wrapper that can't run must not fail the launch: code=%d err=%v", code, err)
	}
	if gaveWay == nil || !strings.Contains(gaveWay.Error(), "shulker-no-such-wrapper") {
		t.Fatalf("the fallback names the wrapper it gave up on: %v", gaveWay)
	}
	readArgs(t, argsFile)
}

func TestRunReportsJavaThatNeverStarted(t *testing.T) {
	_, _, err := Run(Launch{Java: filepath.Join(t.TempDir(), "no-java"), Argv: []string{"x"}}, nil, nil, nil)
	if err == nil {
		t.Fatal("java that can't start is the end of the launch")
	}
}
