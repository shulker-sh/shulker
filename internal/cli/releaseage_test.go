package cli

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/resolve"
)

// weekAfterSodium is 2026-09-08: sodium 1.0.0 (2026-09-01) is 7 days old and old enough, and the
// newer 1.1.0 (2026-09-05) 3 days old and too young.
var weekAfterSodium = time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)

// localDate is how the release age prints the day an instant falls on: in local time.
func localDate(t time.Time) string { return t.Local().Format("2006-01-02") }

func releaseAgeOf(t *testing.T, w out.SecurityWarning) resolve.ReleaseAge {
	t.Helper()
	if w.Protection != "release-age" {
		t.Fatalf("protection %q: %+v", w.Protection, w)
	}
	raw, err := json.Marshal(w.Data)
	if err != nil {
		t.Fatal(err)
	}
	var age resolve.ReleaseAge
	if err := json.Unmarshal(raw, &age); err != nil {
		t.Fatal(err)
	}
	return age
}

func TestAddHoldsBackAVersionYoungerThanTheReleaseAge(t *testing.T) {
	h := newHarness(t)
	h.newer, h.now = true, weekAfterSodium
	h.mustRun(t, "create", "--loader", "fabric")

	env := h.runEnvelope(t, 0, "add", "sodium")
	if v := h.readLock(t).Mods["sodium"].Version; v != "QANobbMI" {
		t.Fatalf("add took %s, want the older QANobbMI", v)
	}
	if len(env.SecurityWarnings) != 1 {
		t.Fatalf("security warnings: %+v", env.SecurityWarnings)
	}
	age := releaseAgeOf(t, env.SecurityWarnings[0])
	want := resolve.Held{Key: "sodium", Took: "1.0.0+mc26.2", Skipped: "1.1.0+mc26.2", SkippedID: "QANobbM2", Age: resolve.Age{Published: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC), AgeDays: 3, Qualifies: time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)}}
	if age.MinReleaseAge != 7 || len(age.Held) != 1 || age.Held[0] != want {
		t.Fatalf("release age: %+v", age)
	}
	if !strings.Contains(strings.Join(env.Warnings, "\n"), "Held back 1 version younger than 7 days.") {
		t.Fatalf("warnings: %q", env.Warnings)
	}
}

func TestUpdateSaysWhatItHeldBackAndHowToTakeIt(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")
	h.newer, h.now = true, weekAfterSodium

	_, stderr := h.mustRunStderr(t, "update")
	for _, want := range []string{
		"Held back 1 version younger than 7 days.",
		"sodium: took 1.0.0+mc26.2 over 1.1.0+mc26.2 (3 days old, qualifies " + localDate(time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)) + ")",
		"Take sodium 1.1.0+mc26.2 now:\n    $ shulker pin sodium QANobbM2\n",
		"Read what shulker checks and why:\n    $ shulker security\n",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("missing %q in %q", want, stderr)
		}
	}
	if v := h.readLock(t).Mods["sodium"].Version; v != "QANobbMI" {
		t.Fatalf("update moved sodium to %s", v)
	}

	h.mustRun(t, "config", "set", "security.minReleaseAge", "0")
	env := h.runEnvelope(t, 0, "update")
	if v := h.readLock(t).Mods["sodium"].Version; v != "QANobbM2" || len(env.SecurityWarnings) != 0 {
		t.Fatalf("with the check off, update took %s and warned %+v", v, env.SecurityWarnings)
	}
}

func TestUpdateNeverMovesBackFromAYoungLockedVersion(t *testing.T) {
	h := newHarness(t)
	h.newer = true
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")
	h.now = weekAfterSodium

	env := h.runEnvelope(t, 0, "update")
	if v := h.readLock(t).Mods["sodium"].Version; v != "QANobbM2" || len(env.SecurityWarnings) != 0 {
		t.Fatalf("update left sodium at %s, warning %+v", v, env.SecurityWarnings)
	}
}

func TestAPinTakesAYoungVersionWithAWarning(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")
	h.newer, h.now = true, weekAfterSodium

	env := h.runEnvelope(t, 0, "pin", "sodium", "QANobbM2")
	if v := h.readLock(t).Mods["sodium"].Version; v != "QANobbM2" {
		t.Fatalf("pin took %s", v)
	}
	if len(env.SecurityWarnings) != 1 || env.SecurityWarnings[0].Message != "sodium 1.1.0+mc26.2 is pinned, so it is taken although it is 3 days old, younger than the 7 days security.minReleaseAge asks." {
		t.Fatalf("security warnings: %+v", env.SecurityWarnings)
	}
	if age := releaseAgeOf(t, env.SecurityWarnings[0]); len(age.Young) != 1 || age.Young[0].Key != "sodium" || age.Young[0].AgeDays != 3 {
		t.Fatalf("release age: %+v", age)
	}
}

func TestAModWithNothingOldEnoughIsRefusedPlainly(t *testing.T) {
	h := newHarness(t)
	h.now = time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	h.mustRun(t, "create", "--loader", "fabric")

	code, stdout, _ := h.run(t, "add", "sodium", "--json")
	e := failureCode(t, stdout)
	if code == 0 || e.Code != "release-too-new" || e.Protection != "release-age" {
		t.Fatalf("exit %d: %s", code, stdout)
	}
	if e.Message != "sodium has no version older than 7 days; its newest, 1.0.0+mc26.2, is 2 days old" || e.Help != "it qualifies on "+localDate(time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC))+"; to take it now, pin it with `shulker add sodium --pin QANobbMI`" {
		t.Fatalf("error: %+v", e)
	}
	h.mustRun(t, "add", "fabric-api", "--pin", "Q7dR8mSH")
	env := h.runEnvelope(t, 0, "add", "sodium", "--pin", "QANobbMI")
	if len(env.SecurityWarnings) != 1 || !strings.HasPrefix(env.SecurityWarnings[0].Message, "sodium 1.0.0+mc26.2 is pinned, so it is taken although it is 2 days old") {
		t.Fatalf("add --pin of a young version: %+v", env.SecurityWarnings)
	}
}

func TestOutdatedListsHeldBackVersionsWithTheirDates(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")
	h.newer, h.now = true, weekAfterSodium

	var env struct {
		Data []resolve.Outdated `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "outdated", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data) != 1 || env.Data[0].ID != "sodium" || env.Data[0].Latest != "1.0.0+mc26.2" || env.Data[0].Held == nil || env.Data[0].Held.SkippedID != "QANobbM2" {
		t.Fatalf("outdated: %+v", env.Data)
	}
	stdout := h.mustRun(t, "outdated")
	if !strings.Contains(stdout, "1.1.0+mc26.2 (held back: 3 days old, qualifies "+localDate(time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC))+")") || !strings.Contains(stdout, "$ shulker pin sodium QANobbM2") || strings.Contains(stdout, "shulker update") {
		t.Fatalf("stdout: %q", stdout)
	}
}

func TestSyncInstallsASourcesYoungVersionAndWarns(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := newHarness(t)
	h.newer = true
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	gitRun(t, h.dir, "init", "-q", "-b", "main")
	gitRun(t, h.dir, "add", ".")
	gitRun(t, h.dir, "commit", "-q", "-m", "one")
	h.now = weekAfterSodium

	env := h.runEnvelope(t, 0, "sync", "file://"+h.dir, "--into", filepath.Join(t.TempDir(), "minecraft"))
	if len(env.SecurityWarnings) != 1 {
		t.Fatalf("security warnings: %+v", env.SecurityWarnings)
	}
	age := releaseAgeOf(t, env.SecurityWarnings[0])
	if len(age.Young) != 1 || age.Young[0].Key != "sodium" || age.Young[0].Version != "1.1.0+mc26.2" || age.Young[0].AgeDays != 3 {
		t.Fatalf("release age: %+v", age)
	}
	if !strings.HasPrefix(env.SecurityWarnings[0].Message, "The source locks 1 version younger than 7 days, installed as its author chose them.") {
		t.Fatalf("message: %q", env.SecurityWarnings[0].Message)
	}
}

func TestLockHoldsBackANewEntrysYoungVersion(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "add", "fabric-api")
	h.editManifest(t, func(m map[string]any) {
		m["requires"].(map[string]any)["sodium"] = map[string]any{}
	})
	h.newer, h.now = true, weekAfterSodium

	env := h.runEnvelope(t, 0, "lock")
	if v := h.readLock(t).Mods["sodium"].Version; v != "QANobbMI" {
		t.Fatalf("lock took %s", v)
	}
	if len(env.SecurityWarnings) != 1 || len(releaseAgeOf(t, env.SecurityWarnings[0]).Held) != 1 {
		t.Fatalf("security warnings: %+v", env.SecurityWarnings)
	}
}

// youngCozy publishes cozy 2.0.0 on 2026-09-05, 3 days before weekAfterSodium.
func youngCozy(t *testing.T, h *harness) {
	t.Helper()
	next := hostedMrpack(t, h, "cozy-2.0.0.mrpack", "2.0.0")
	h.modrinthPacks["COZYpack"].versions = append(h.modrinthPacks["COZYpack"].versions, modrinthPackVersion{id: "cozyV200", number: "2.0.0", published: "2026-09-05T00:00:00Z", archive: next})
	h.now = weekAfterSodium
}

func TestOutdatedMarksAHostedModpacksHeldBackVersion(t *testing.T) {
	h, _ := hostedProject(t)
	h.mustRun(t, "add", "cozy")
	youngCozy(t, h)

	var env struct {
		Data []resolve.Outdated `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "outdated", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data) != 1 || env.Data[0].ID != "cozy" || !env.Data[0].Modpack || env.Data[0].Held == nil || env.Data[0].Held.SkippedID != "cozyV200" || env.Data[0].Latest != "1.0.0" {
		t.Fatalf("outdated: %+v", env.Data)
	}
}

func TestSyncHoldsBackANewHostedModpacksYoungVersion(t *testing.T) {
	h := newInPlace(t)
	archive := hostedMrpack(t, h, "cozy-1.0.0.mrpack", "1.0.0")
	h.modrinthPacks = map[string]*modrinthPack{"COZYpack": {slug: "cozy", versions: []modrinthPackVersion{{id: "cozyV100", number: "1.0.0", published: "2026-09-01T00:00:00Z", archive: archive}}}}
	youngCozy(t, h)
	h.editManifest(t, func(m map[string]any) {
		m["requires"] = map[string]any{"cozy": map[string]any{"type": "modpack", "provider": "modrinth", "project": "COZYpack"}}
	})

	env := h.runEnvelope(t, 0, "sync")
	if v := h.readLock(t).Modpacks["cozy"].Version; v != "cozyV100" {
		t.Fatalf("sync took cozy %s", v)
	}
	if len(env.SecurityWarnings) != 1 {
		t.Fatalf("security warnings: %+v", env.SecurityWarnings)
	}
	if held := releaseAgeOf(t, env.SecurityWarnings[0]).Held; len(held) != 1 || held[0].Key != "cozy" || held[0].SkippedID != "cozyV200" {
		t.Fatalf("held: %+v", held)
	}
}
