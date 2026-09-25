//go:build windows

package modpack

import "os/exec"

func killGroup(*exec.Cmd) {}
