package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/out"
)

type checkEnvelope struct {
	out.Envelope
	Data struct {
		Scopes   []string    `json:"scopes"`
		Problems []out.Error `json:"problems"`
	} `json:"data"`
}

func runCheck(t *testing.T, h *harness, args ...string) (int, checkEnvelope) {
	t.Helper()
	code, stdout, _ := h.run(t, append([]string{"check", "--json"}, args...)...)
	var env checkEnvelope
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("%v: %s", err, stdout)
	}
	return code, env
}

func (env checkEnvelope) problem(code string) *out.Error {
	for i := range env.Data.Problems {
		if env.Data.Problems[i].Code == code {
			return &env.Data.Problems[i]
		}
	}
	return nil
}

func treeDigest(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		rel, _ := filepath.Rel(dir, path)
		files[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func (h *harness) editLock(t *testing.T, edit func(l *lock.Lock)) {
	t.Helper()
	l := h.readLock(t)
	edit(l)
	if err := l.Save(filepath.Join(h.dir, lock.FileName)); err != nil {
		t.Fatal(err)
	}
}

func TestCheckPassesACleanProjectAndWritesNothing(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")
	if err := os.RemoveAll(h.cache); err != nil {
		t.Fatal(err)
	}
	before := treeDigest(t, h.dir)

	stdout := h.mustRun(t, "check")
	if !strings.Contains(stdout, "No problems") {
		t.Fatalf("check output: %s", stdout)
	}
	code, env := runCheck(t, h)
	if code != 0 || !env.OK || len(env.Data.Problems) != 0 {
		t.Fatalf("clean check: exit %d %+v", code, env)
	}

	after := treeDigest(t, h.dir)
	if len(after) != len(before) {
		t.Fatalf("check changed the project's files:\nbefore %v\nafter  %v", before, after)
	}
	for rel, sum := range before {
		if after[rel] != sum {
			t.Fatalf("check changed %s", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build")); !os.IsNotExist(err) {
		t.Fatalf("check made build/: %v", err)
	}
}

func TestCheckFailsAStaleLockWithoutRelocking(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")
	h.editManifest(t, func(m map[string]any) {
		m["requires"].(map[string]any)["sodium"] = map[string]any{"side": "server"}
	})
	lockBefore, _ := os.ReadFile(filepath.Join(h.dir, lock.FileName))

	code, env := runCheck(t, h)
	stale := env.problem("lock-stale")
	if code == 0 || env.Error == nil || env.Error.Code != "check-failed" || stale == nil || len(stale.Items) != 1 || stale.Items[0] != "sodium: side client -> server" {
		t.Fatalf("stale check: exit %d %+v", code, env)
	}
	if lockAfter, _ := os.ReadFile(filepath.Join(h.dir, lock.FileName)); string(lockAfter) != string(lockBefore) {
		t.Fatal("check relocked")
	}
}

func TestCheckReportsAMissingDependencyAsInstallDoes(t *testing.T) {
	h := newHarness(t)
	h.jars["sodium"] = makeJarWith(t, "sodium", h.jars["sodium"].filename, "client", `"depends":{"fabricloader":">=0.17","fabric-api":"*"}`)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")
	h.editLock(t, func(l *lock.Lock) { delete(l.Mods, "fabric-api") })

	_, _, installErr := h.run(t, "install")
	code, _, stderr := h.run(t, "check")
	if code == 0 || !strings.Contains(stderr, "shulker ignore sodium fabric-api") || !strings.Contains(installErr, "shulker ignore sodium fabric-api") {
		t.Fatalf("check: exit %d\n%s", code, stderr)
	}
	line := "sodium 1.0.0 requires fabric-api"
	if !strings.Contains(stderr, line) || !strings.Contains(installErr, line) {
		t.Fatalf("check and install must print the same problem line:\ncheck: %s\ninstall: %s", stderr, installErr)
	}
	if _, env := runCheck(t, h); env.problem("validation-failed") == nil {
		t.Fatalf("check --json: %+v", env)
	}
}

func TestCheckNamesAManualDownloadMissingFromDownloads(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	downloads := filepath.Join(h.dir, "downloads")
	os.MkdirAll(downloads, 0o755)
	os.WriteFile(filepath.Join(downloads, "nodist-1.0.0.jar"), h.jars["nodist"].data, 0o644)
	h.mustRun(t, "add", "nodist")
	os.RemoveAll(downloads)
	os.RemoveAll(h.cache)

	code, env := runCheck(t, h)
	missing := env.problem("missing-files")
	if code == 0 || missing == nil || len(missing.Items) != 1 || !strings.Contains(missing.Items[0], "nodist-1.0.0.jar") {
		t.Fatalf("check with a manual download gone: exit %d %+v", code, env)
	}
	if !strings.Contains(strings.Join(env.Error.Items, "\n"), "nodist-1.0.0.jar") {
		t.Fatalf("the run's items must list every problem: %+v", env.Error)
	}
	if code, env = runCheck(t, h, "--strict"); code == 0 || len(env.Data.Problems) != 1 || len(env.Warnings) != 0 {
		t.Fatalf("a file that can't be fetched is one problem, not also an unchecked-metadata warning: exit %d %+v", code, env)
	}
}

func TestCheckChecksEverySide(t *testing.T) {
	h := newHarness(t)
	h.jars["sodium"] = makeJarWith(t, "sodium", h.jars["sodium"].filename, "*", `"depends":{"fabricloader":">=0.17","fabric-api":"*"}`)
	h.mustRun(t, "create", "--loader", "fabric")
	h.editManifest(t, func(m map[string]any) { m["server"] = map[string]any{} })
	h.mustRun(t, "add", "sodium")
	h.editLock(t, func(l *lock.Lock) {
		sodium, api := l.Mods["sodium"], l.Mods["fabric-api"]
		sodium.Side, api.Side = "both", "client"
		l.Mods["sodium"], l.Mods["fabric-api"] = sodium, api
	})

	code, env := runCheck(t, h)
	v := env.problem("validation-failed")
	if code == 0 || v == nil || len(v.Items) != 1 || !strings.Contains(v.Items[0], "server doesn't place") {
		t.Fatalf("server-only problem: exit %d %+v", code, env)
	}
}

func TestCheckStrictFailsOnWarnings(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")
	h.editManifest(t, func(m map[string]any) {
		m["ignore"] = []any{map[string]any{"rule": "depends", "mod": "sodium", "on": "nothing", "declared": "*", "note": "stale"}}
	})

	code, env := runCheck(t, h)
	if code != 0 || len(env.Warnings) != 1 || !strings.Contains(env.Warnings[0], "matched nothing") {
		t.Fatalf("a warning alone passes: exit %d %+v", code, env)
	}
	code, env = runCheck(t, h, "--strict")
	if code == 0 || env.problem("strict-warnings") == nil {
		t.Fatalf("--strict fails on a warning: exit %d %+v", code, env)
	}
}

func (h *harness) isCached(sha512 string) bool {
	_, err := os.Stat(filepath.Join(h.cache, "objects", sha512[:2], sha512))
	return err == nil
}

func TestCheckRunsOnlyTheScopesNamed(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "resourcepack", "add", "fresh-animations")
	os.RemoveAll(h.cache)

	code, env := runCheck(t, h, "lock")
	if code != 0 || strings.Join(env.Data.Scopes, ",") != "lock" || h.isCached(h.jars["sodium"].sha512) {
		t.Fatalf("lock alone downloads nothing: exit %d %+v", code, env)
	}
	code, env = runCheck(t, h, "deps", "lock")
	if code != 0 || strings.Join(env.Data.Scopes, ",") != "lock,deps" || !h.isCached(h.jars["sodium"].sha512) || h.isCached(h.jars["fresh-animations"].sha512) {
		t.Fatalf("deps fetches the mod jars alone: exit %d %+v", code, env)
	}
	code, env = runCheck(t, h)
	if code != 0 || strings.Join(env.Data.Scopes, ",") != "lock,files,deps" || !h.isCached(h.jars["fresh-animations"].sha512) {
		t.Fatalf("the default fetches every file: exit %d %+v", code, env)
	}

	for _, args := range [][]string{{"nope"}, {"lock", "--all"}} {
		if e := runError(t, h, append([]string{"check"}, args...)...); e.Code != "usage" {
			t.Fatalf("check %v: %+v", args, e)
		}
	}
}

func TestCheckServerOnlyWhenNamedOrAll(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")

	if e := runError(t, h, "check", "server"); e.Code != "no-side" {
		t.Fatalf("server on a client-only project: %+v", e)
	}
	if code, env := runCheck(t, h, "--all"); code != 0 || strings.Join(env.Data.Scopes, ",") != "lock,files,deps" {
		t.Fatalf("--all leaves out a server the project doesn't declare: exit %d %+v", code, env)
	}

	h.editManifest(t, func(m map[string]any) { m["server"] = map[string]any{} })
	h.mustRun(t, "lock")
	lockBefore, _ := os.ReadFile(filepath.Join(h.dir, lock.FileName))
	code, env := runCheck(t, h, "--all")
	if code != 0 || strings.Join(env.Data.Scopes, ",") != "lock,files,deps,server" {
		t.Fatalf("--all checks the server: exit %d %+v", code, env)
	}
	if lockAfter, _ := os.ReadFile(filepath.Join(h.dir, lock.FileName)); string(lockAfter) != string(lockBefore) {
		t.Fatal("check server locked the server jar")
	}
}

func TestCheckAnnotatesEachProblemInGitHubActions(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")
	h.editManifest(t, func(m map[string]any) {
		m["requires"].(map[string]any)["sodium"] = map[string]any{"side": "server"}
		m["ignore"] = []any{map[string]any{"rule": "depends", "mod": "sodium", "on": "nothing", "declared": "*", "note": "stale"}}
	})
	stale := "::error title=shulker.lock does not match shulker.json (lock-stale)::sodium: side client -> server\n"
	warning := "::warning::ignore entry 1 (depends: sodium on nothing) matched nothing\n"

	_, _, stderr := h.run(t, "check")
	if strings.Contains(stderr, "::error") {
		t.Fatalf("annotations outside GitHub Actions: %s", stderr)
	}
	t.Setenv("GITHUB_ACTIONS", "true")
	code, stdout, stderr := h.run(t, "check", "--json")
	if code == 0 || !json.Valid([]byte(stdout)) || !strings.Contains(stderr, stale) || !strings.Contains(stderr, warning) || strings.Count(stderr, "::error") != 1 {
		t.Fatalf("annotations in GitHub Actions: exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if _, _, stderr = h.run(t, "check", "--no-annotations"); strings.Contains(stderr, "::") {
		t.Fatalf("--no-annotations: %s", stderr)
	}
	t.Setenv("GITHUB_ACTIONS", "")
	if _, _, stderr = h.run(t, "check", "--annotations"); !strings.Contains(stderr, stale) {
		t.Fatalf("--annotations outside GitHub Actions: %s", stderr)
	}
}
