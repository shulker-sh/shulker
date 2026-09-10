//go:build windows

package server

import "os/exec"

func detach(*exec.Cmd) {}
