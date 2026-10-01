//go:build !darwin && !linux

package sandbox

import "errors"

// Available is nil when this machine can sandbox a game, and otherwise says why not.
func Available() error { return errors.New("there is no sandbox for this system yet") }

// Exec replaces this process with Java under the policy, and only returns when that fails.
func Exec(Policy, string, []string) error { return Available() }
