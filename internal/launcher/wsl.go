package launcher

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// wslAppData is the Windows user's %APPDATA% as a path this Linux side can read, when shulker runs
// under WSL, else "". Windows launchers keep their accounts there, out of reach of Linux's own
// defaults.
var wslAppData = sync.OnceValue(func() string {
	if runtime.GOOS != "linux" || os.Getenv("WSL_DISTRO_NAME") == "" {
		return ""
	}
	cmdExe, err := exec.LookPath("cmd.exe")
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cmdExe, "/d", "/c", "echo %APPDATA%")
	// cmd.exe can't start in a Linux folder, so it starts in its own.
	cmd.Dir = filepath.Dir(cmdExe)
	raw, err := cmd.Output()
	windows := strings.TrimSpace(string(raw))
	if err != nil || windows == "" || strings.Contains(windows, "%") {
		return ""
	}
	raw, err = exec.CommandContext(ctx, "wslpath", "-u", windows).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
})

// wslAccountsDir is where the Windows side keeps e's accounts, for a WSL user whose launcher runs
// on Windows, or "" when there is no such folder.
func (e *Entry) wslAccountsDir() string {
	appData := wslAppData()
	if e.WindowsAppData == "" || appData == "" {
		return ""
	}
	dir := filepath.Join(appData, e.WindowsAppData)
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	return dir
}
