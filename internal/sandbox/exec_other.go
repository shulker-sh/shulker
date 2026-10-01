//go:build !darwin

package sandbox

import "errors"

// Supported reports whether this machine can sandbox a game.
func Supported() bool { return false }

// Exec replaces this process with Java under the policy, and only returns when that fails.
func Exec(Policy, string, []string) error {
	return errors.New("the sandbox isn't available on this system")
}
