//go:build windows

package pack

import "os/exec"

func killGroup(*exec.Cmd) {}
