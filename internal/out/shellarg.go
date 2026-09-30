package out

import (
	"regexp"
	"runtime"
	"strings"
)

var plainShellArg = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,-]+$`)

// ShellArg quotes s as one word of a command the user copies into their shell. On Windows it
// takes double quotes, which cmd and PowerShell both read, and a Windows path can't hold one.
func ShellArg(s string) string {
	return shellArg(s, runtime.GOOS)
}

func shellArg(s, goos string) string {
	if plainShellArg.MatchString(s) {
		return s
	}
	if goos == "windows" {
		return `"` + s + `"`
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
