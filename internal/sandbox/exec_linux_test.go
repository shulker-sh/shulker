package sandbox

import (
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// The arguments are only worth what bubblewrap makes of them, so this runs a shell under it.
func TestBubblewrapConfinesARealProcess(t *testing.T) {
	if err := Available(); err != nil {
		t.Skipf("no sandbox on this machine: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	gameDir, err := os.MkdirTemp(home, ".shulker-sandbox-test-")
	if err != nil {
		t.Skipf("can't make a folder under home: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(gameDir) })
	outside := filepath.Join(home, ".shulker-sandbox-test-outside")
	secret := filepath.Join(home, ".shulker-sandbox-test-secret")
	if err := os.WriteFile(secret, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(outside); os.Remove(secret) })
	for _, dir := range []string{"mods", ".shulker/logs"} {
		if err := os.MkdirAll(filepath.Join(gameDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(gameDir, "instance.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gameDir, ".shulker", "pre-launch"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	p, err := Derive("/bin/sh", []string{"-c", "true", "Main", "--gameDir", gameDir}, Options{ProtectFiles: []string{filepath.Join(gameDir, "instance.json")}})
	if err != nil {
		t.Fatal(err)
	}
	if err := makeBound(p); err != nil {
		t.Fatal(err)
	}
	gameDir = resolve(gameDir)
	script := strings.Join([]string{
		`try() { if sh -c "$2" >/dev/null 2>&1; then echo "$1=allowed"; else echo "$1=blocked"; fi; }`,
		`try read-secret "cat '` + secret + `'"`,
		`try write-home "touch '` + outside + `'"`,
		`try write-instance "touch '` + gameDir + `/options.txt'"`,
		`try new-folder "mkdir '` + gameDir + `/crash-reports'"`,
		`try read-instance "cat '` + gameDir + `/instance.json'"`,
		`try write-mods "touch '` + gameDir + `/mods/evil.jar'"`,
		`try write-hook "sh -c 'echo x >> \"` + gameDir + `/.shulker/pre-launch\"'"`,
		`try write-logs "touch '` + gameDir + `/.shulker/logs/latest.log'"`,
		`try write-launcher-file "sh -c 'echo x >> \"` + gameDir + `/instance.json\"'"`,
		`try write-tmp "touch /tmp/shulker-sandbox-test"`,
		`try write-etc "touch /etc/shulker-sandbox-test"`,
		`try read-system "cat /etc/hostname"`,
		`try see-outside "test -d /proc/$PPID/root && test $$ -gt 50"`,
	}, "\n")
	bwrap, _ := exec.LookPath("bwrap")
	args := append(BwrapArgs(p, desktop()), "/bin/sh", "-c", script)
	cmd := exec.Command(bwrap, args...)
	cmd.Dir = gameDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bwrap: %v\n%s", err, output)
	}
	want := map[string]string{
		// Home is an empty folder of the sandbox's own, so a write there succeeds and is gone with the
		// game; the check below is that nothing reached the real one.
		"read-secret": "blocked", "write-home": "allowed",
		"write-instance": "allowed", "new-folder": "allowed", "read-instance": "allowed",
		"write-mods": "blocked", "write-hook": "blocked", "write-logs": "allowed", "write-launcher-file": "blocked",
		"write-tmp": "allowed", "write-etc": "blocked", "read-system": "allowed",
		// In its own process namespace the shell is among the first few processes, not the system's.
		"see-outside": "blocked",
	}
	got := map[string]string{}
	for _, line := range strings.Fields(string(output)) {
		if name, result, ok := strings.Cut(line, "="); ok {
			got[name] = result
		}
	}
	for name, result := range want {
		if got[name] != result {
			t.Errorf("%s: %s, want %s", name, got[name], result)
		}
	}
	if _, err := os.Stat(outside); err == nil {
		t.Error("the sandboxed shell wrote outside the instance")
	}
	if _, err := os.Stat("/tmp/shulker-sandbox-test"); err == nil {
		t.Error("the sandbox's /tmp is the system's own")
	}
}

// Scoping can only be checked from inside it, and it holds for good once asked for, so the test
// runs itself twice over: once to scope and exec, and once, scoped, to dial.
func TestScopedProcessCantReachAnAbstractSocketMadeOutside(t *testing.T) {
	const name = "@shulker-sandbox-test"
	switch os.Getenv("SHULKER_SANDBOX_TEST") {
	case "scope":
		runtime.LockOSThread()
		if !scopeSockets("/proc/self/exe") {
			os.Exit(3)
		}
		os.Setenv("SHULKER_SANDBOX_TEST", "dial")
		t.Fatal(syscall.Exec("/proc/self/exe", os.Args, os.Environ()))
	case "dial":
		if conn, err := net.Dial("unix", name); err == nil {
			conn.Close()
			os.Exit(0)
		}
		os.Exit(1)
	}
	listener, err := net.Listen("unix", name)
	if err != nil {
		t.Skipf("no abstract sockets here: %v", err)
	}
	defer listener.Close()
	if conn, err := net.Dial("unix", name); err != nil {
		t.Fatalf("unscoped, the socket is reachable: %v", err)
	} else {
		conn.Close()
	}
	cmd := exec.Command("/proc/self/exe", "-test.run", "TestScopedProcessCantReachAnAbstractSocketMadeOutside")
	cmd.Env = append(os.Environ(), "SHULKER_SANDBOX_TEST=scope")
	err = cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("a scoped process reached an abstract socket made outside it (err %v)", err)
	}
	switch exit.ExitCode() {
	case 3:
		t.Skip("this kernel has no Landlock scoping (Linux 6.12)")
	case 1:
	default:
		t.Fatalf("helper exited %d", exit.ExitCode())
	}
}
