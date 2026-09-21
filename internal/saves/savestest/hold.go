// Package savestest holds a world open the way a running game does, from a child process, since
// POSIX record locks never conflict with their own process's.
package savestest

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const holdEnv = "SHULKER_TEST_HOLD_WORLD"

// Main holds the world named in the environment and returns false, when this test binary was
// started by Hold; a TestMain calls it first and exits on false.
func Main() bool {
	world := os.Getenv(holdEnv)
	if world == "" {
		return true
	}
	release, err := lock(filepath.Join(world, "session.lock"))
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Println("held")
	bufio.NewReader(os.Stdin).ReadString('\n')
	release()
	return false
}

// Hold locks world's session.lock from a child process until the test ends.
func Hold(t *testing.T, world string) {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), holdEnv+"="+world)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stdin.Close()
		cmd.Wait()
	})
	if line, _ := bufio.NewReader(stdout).ReadString('\n'); line != "held\n" {
		t.Fatalf("holding %s: %q", world, line)
	}
}
