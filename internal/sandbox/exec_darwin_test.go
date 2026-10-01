package sandbox

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The profile is only worth what the system makes of it, so this runs a shell under the real one.
func TestTheProfileConfinesARealProcess(t *testing.T) {
	if !Supported() {
		t.Skip("no sandbox-exec on this machine")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	// The game directory has to sit under /Users for the deny to matter, and a test's own temp
	// folder is under /private/var, which every process may write.
	gameDir, err := os.MkdirTemp(home, ".shulker-sandbox-test-")
	if err != nil {
		t.Skipf("can't make a folder under home: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(gameDir) })
	outside := filepath.Join(home, ".shulker-sandbox-test-outside")
	t.Cleanup(func() { os.Remove(outside) })
	for _, dir := range []string{"mods", ".shulker/logs"} {
		if err := os.MkdirAll(filepath.Join(gameDir, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(gameDir, "instance.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	argv := []string{"-c", "true", "Main", "--gameDir", gameDir}
	p, err := Derive("/bin/sh", argv, Options{ProtectFiles: []string{filepath.Join(gameDir, "instance.json")}})
	if err != nil {
		t.Fatal(err)
	}
	gameDir = resolve(gameDir)
	script := strings.Join([]string{
		`try() { if sh -c "$2" >/dev/null 2>&1; then echo "$1=allowed"; else echo "$1=blocked"; fi; }`,
		`try list-home "ls '` + home + `'"`,
		`try write-home "touch '` + outside + `'"`,
		`try write-instance "touch '` + gameDir + `/options.txt'"`,
		`try read-instance "cat '` + gameDir + `/instance.json'"`,
		`try write-mods "touch '` + gameDir + `/mods/evil.jar'"`,
		`try write-shulker "touch '` + gameDir + `/.shulker/java'"`,
		`try write-logs "touch '` + gameDir + `/.shulker/logs/latest.log'"`,
		`try write-launcher-file "sh -c 'echo x >> \"` + gameDir + `/instance.json\"'"`,
		`try read-system "cat /etc/hosts"`,
	}, "\n")
	cmd := exec.Command(sandboxExec, "-p", Profile(p), "/bin/sh", "-c", script)
	cmd.Dir = gameDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sandbox-exec: %v\n%s", err, output)
	}
	want := map[string]string{
		"list-home": "blocked", "write-home": "blocked",
		"write-instance": "allowed", "read-instance": "allowed",
		"write-mods": "blocked", "write-shulker": "blocked", "write-logs": "allowed", "write-launcher-file": "blocked",
		"read-system": "allowed",
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
}
