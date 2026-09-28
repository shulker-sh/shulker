//go:build unix

package cli

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

// ptyTerminal is the far side of a pty: it keeps what the program wrote and answers the queries a
// real terminal answers, background colour, cursor position and device attributes, so the program
// waits for none of them.
type ptyTerminal struct {
	mu     sync.Mutex
	f      *os.File
	output bytes.Buffer
}

func (p *ptyTerminal) read() {
	buf := make([]byte, 4096)
	for {
		n, err := p.f.Read(buf)
		chunk := buf[:n]
		p.mu.Lock()
		p.output.Write(chunk)
		p.mu.Unlock()
		if bytes.Contains(chunk, []byte("\x1b]11;?")) {
			p.f.WriteString("\x1b]11;rgb:0000/0000/0000\x1b\\")
		}
		if bytes.Contains(chunk, []byte("\x1b[6n")) {
			p.f.WriteString("\x1b[1;1R")
		}
		if bytes.Contains(chunk, []byte("\x1b[c")) {
			p.f.WriteString("\x1b[?62;22c")
		}
		if err != nil {
			return
		}
	}
}

func (p *ptyTerminal) String() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.output.String()
}

func (p *ptyTerminal) waitFor(t *testing.T, want string) {
	t.Helper()
	for deadline := time.Now().Add(30 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		if strings.Contains(p.String(), want) {
			return
		}
	}
	t.Fatalf("no %q in:\n%s", want, p.String())
}

// pendingManualProject is a project whose one mod waits for a manual download, so an install
// reaches the download wait without touching the network.
func pendingManualProject(t *testing.T, jar []byte) string {
	t.Helper()
	dir := t.TempDir()
	sum := sha1.Sum(jar)
	manifest := map[string]any{
		"$schema":   "https://shulker.sh/schema/v1/manifest.json",
		"name":      "pty-pack",
		"minecraft": "26.2",
		"requires":  map[string]any{"handmade": map[string]any{}},
		"client":    map[string]any{},
	}
	lock := map[string]any{
		"$schema":   "https://shulker.sh/schema/v1/lock.json",
		"minecraft": "26.2",
		"java":      map[string]any{"major": 25, "component": "java-runtime-epsilon"},
		"modpacks":  map[string]any{}, "resourcepacks": map[string]any{}, "shaders": map[string]any{}, "datapacks": map[string]any{},
		"players": []any{},
		"mods": map[string]any{"handmade": map[string]any{
			"provider": "curseforge", "project": "355424", "channel": "release", "version": "6721093",
			"versionNumber": "1.0", "filename": "handmade-1.0.jar", "url": nil,
			"page":   "https://www.curseforge.com/minecraft/mc-mods/handmade/files/6721093",
			"sha512": "", "sha1": hex.EncodeToString(sum[:]), "side": "client", "requiredBy": []any{}, "aliases": map[string]any{},
		}},
	}
	for name, v := range map[string]any{"shulker.json": manifest, "shulker.lock": lock} {
		data, _ := json.MarshalIndent(v, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func handmadeJar(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("fabric.mod.json")
	w.Write([]byte(`{"schemaVersion": 1, "id": "handmade", "version": "1.0"}`))
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// TestDownloadWaitInAPty runs the built binary at a real terminal: enter checks without ending
// the wait while the file is missing, the wait ends by itself once the file lands, and esc skips.
func TestDownloadWaitInAPty(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the binary")
	}
	bin := filepath.Join(t.TempDir(), "shulker")
	if msg, err := exec.Command("go", "build", "-o", bin, "shulker.sh/shulker").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, msg)
	}
	jar := handmadeJar(t)
	start := func(dir string) (*exec.Cmd, *ptyTerminal) {
		home := t.TempDir()
		cmd := exec.Command(bin, "install")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "TERM=xterm-256color",
			"SHULKER_CONFIG="+filepath.Join(home, "config.json"), "SHULKER_CACHE="+filepath.Join(home, "cache"), "SHULKER_DATA="+filepath.Join(home, "data"))
		f, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 40, Cols: 120})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cmd.Process.Kill(); f.Close() })
		term := &ptyTerminal{f: f}
		go term.read()
		return cmd, term
	}
	exited := func(cmd *exec.Cmd) chan error {
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		return done
	}

	t.Run("enter and a dropped file", func(t *testing.T) {
		dir := pendingManualProject(t, jar)
		cmd, term := start(dir)
		term.waitFor(t, "Press Enter")
		done := exited(cmd)
		term.f.WriteString("\r")
		select {
		case err := <-done:
			t.Fatalf("enter ended the wait with the file missing: %v\n%s", err, term)
		case <-time.After(time.Second):
		}
		os.WriteFile(filepath.Join(dir, "downloads", "handmade (1).jar"), jar, 0o644)
		term.f.WriteString("\r")
		select {
		case err := <-done:
			if err != nil || !strings.Contains(term.String(), "Built client") {
				t.Fatalf("install after the file lands: %v\n%s", err, term)
			}
		case <-time.After(30 * time.Second):
			t.Fatalf("the wait never saw the file:\n%s", term)
		}
		data, _ := os.ReadFile(filepath.Join(dir, "shulker.lock"))
		if strings.Contains(string(data), `"sha512": ""`) {
			t.Fatalf("the dropped file fills the pending mod:\n%s", data)
		}
	})

	t.Run("esc", func(t *testing.T) {
		cmd, term := start(pendingManualProject(t, jar))
		term.waitFor(t, "Press Enter")
		done := exited(cmd)
		term.f.WriteString("\x1b")
		select {
		case err := <-done:
			if err != nil || !strings.Contains(term.String(), "handmade is left out") || !strings.Contains(term.String(), "Built client") {
				t.Fatalf("esc skips the file and install goes on: %v\n%s", err, term)
			}
		case <-time.After(30 * time.Second):
			t.Fatalf("esc didn't end the wait:\n%s", term)
		}
	})
}
