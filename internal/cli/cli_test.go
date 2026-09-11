package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Execute(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestVersionHuman(t *testing.T) {
	code, stdout, _ := run(t, "version")
	if code != out.ExitOK {
		t.Fatalf("exit %d", code)
	}
	if !strings.HasPrefix(stdout, "shulker dev (") {
		t.Fatalf("unexpected output %q", stdout)
	}
}

func TestVersionJSON(t *testing.T) {
	code, stdout, _ := run(t, "version", "--json")
	if code != out.ExitOK {
		t.Fatalf("exit %d", code)
	}
	var env out.Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if !env.OK || env.Command != "version" || env.Error != nil || env.Warnings == nil {
		t.Fatalf("unexpected envelope %+v", env)
	}
	data := env.Data.(map[string]any)
	if data["version"] != "dev" {
		t.Fatalf("unexpected data %+v", data)
	}
}

func TestHelpLinksDocs(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"add", "--help"}, {"target", "add", "--help"}} {
		code, stdout, _ := run(t, args...)
		if code != out.ExitOK || !strings.HasSuffix(stdout, helpFooter) {
			t.Fatalf("%v: code=%d stdout=%q", args, code, stdout)
		}
	}
}

func TestUnknownCommandJSON(t *testing.T) {
	code, stdout, stderr := run(t, "bogus", "--json")
	if code != out.ExitUsage {
		t.Fatalf("exit %d", code)
	}
	if stderr != "" {
		t.Fatalf("stderr should be empty in json mode, got %q", stderr)
	}
	var env out.Envelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if env.OK || env.Error == nil || env.Error.Code != "usage" {
		t.Fatalf("unexpected envelope %+v", env)
	}
}

func TestUnknownCommandHuman(t *testing.T) {
	code, stdout, stderr := run(t, "bogus")
	if code != out.ExitUsage || stdout != "" || !strings.HasPrefix(stderr, "shulker: ") {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{"My Pack": "my-pack", "tmp.KEnt9tWC1o": "tmp.kent9twc1o", "---": "shulker-project", "west_coast SMP!": "west_coast-smp"}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProjectFileCodes(t *testing.T) {
	dir := t.TempDir()
	if code, stdout, _ := run(t, "build", "-C", dir, "--json"); code != out.ExitError || failureCode(t, stdout).Code != "manifest-not-found" {
		t.Fatalf("no shulker.json: exit %d %s", code, stdout)
	}
	if err := os.WriteFile(filepath.Join(dir, "shulker.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, stdout, _ := run(t, "build", "-C", dir, "--json"); code != out.ExitError || failureCode(t, stdout).Code != "manifest-invalid" {
		t.Fatalf("invalid shulker.json: exit %d %s", code, stdout)
	}
}
