// Package proc reads how a program shulker ran came to an end.
package proc

import (
	"errors"
	"os/exec"
)

// ExitCode is the status a finished program left. A program that ran and failed is a status, not
// an error: only one that never started, or couldn't be waited on, is an error.
func ExitCode(err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return signed(exit.ExitCode()), nil
	}
	return 0, err
}

// signed reads a status as the program meant it: Windows reports its 32-bit exit code unsigned,
// so a Java System.exit(-1) arrives as 4294967295.
func signed(code int) int { return int(int32(code)) }
