package sync

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

// takedownHarness is a harness with sodium locked and a registered instance, survival, synced from
// the project, on a clock the test moves.
func takedownHarness(t *testing.T) (h *harness, into string, now *time.Time) {
	h = newHarness(t)
	h.add("sodium")
	into = filepath.Join(t.TempDir(), "survival")
	h.register(config.Instance{ID: "survival", Dir: into, Source: h.dir})
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	h.e.Now = func() time.Time { return at }
	return h, into, &at
}

// takedownWarning is the one security warning from the takedowns protection, if there is one.
func takedownWarning(t *testing.T, h *harness) (out.SecurityWarning, bool) {
	t.Helper()
	var found []out.SecurityWarning
	for _, w := range h.env.Security {
		if w.Protection == "takedowns" {
			found = append(found, w)
		}
	}
	if len(found) > 1 {
		t.Fatalf("one takedown warning, got %+v", found)
	}
	if len(found) == 0 {
		return out.SecurityWarning{}, false
	}
	return found[0], true
}

func (h *harness) takeDown(v provider.Version) {
	h.env.Modrinth.Files = slices.DeleteFunc(h.env.Modrinth.Files, func(f provider.Version) bool { return f.ID == v.ID })
}

func TestSyncChecksForTakedownsAtMostOnceADay(t *testing.T) {
	h, into, now := takedownHarness(t)

	h.mustSync(into, Request{})
	if n := h.env.Modrinth.Requests["Filed"]; n != 1 {
		t.Fatalf("the first sync asks once, asked %d times", n)
	}
	if _, warned := takedownWarning(t, h); warned {
		t.Fatalf("nothing is gone: %+v", h.env.Security)
	}
	if st := instance.LoadState(into); st.Takedowns == nil || st.Takedowns.CheckedAt != "2026-09-29T12:00:00Z" || len(st.Takedowns.Files) != 0 {
		t.Fatalf("the check is recorded: %+v", st.Takedowns)
	}

	h.takeDown(h.sodium)
	*now = now.Add(23 * time.Hour)
	h.mustSync(into, Request{})
	if n := h.env.Modrinth.Requests["Filed"]; n != 1 {
		t.Fatalf("a sync within the day doesn't ask again, asked %d times", n)
	}
	if _, warned := takedownWarning(t, h); warned {
		t.Fatal("within the day the sync goes on the last check")
	}

	*now = now.Add(2 * time.Hour)
	h.mustSync(into, Request{})
	if n := h.env.Modrinth.Requests["Filed"]; n != 2 {
		t.Fatalf("a sync a day on asks again, asked %d times", n)
	}
	w, warned := takedownWarning(t, h)
	if !warned {
		t.Fatalf("a gone file warns: %v", h.env.Warnings)
	}
	for _, want := range []string{"1 locked file is gone from its provider", "sodium: Modrinth no longer has the locked version, 0.9.2", "used by survival"} {
		if !strings.Contains(w.Message, want) {
			t.Errorf("warning %q lacks %q", w.Message, want)
		}
	}
	var commands []string
	for _, n := range w.Nudges {
		commands = append(commands, n.Command)
	}
	if want := []string{"shulker audit sodium", "shulker update sodium", "shulker remove sodium", "shulker security"}; !slices.Equal(commands, want) {
		t.Fatalf("nudges %q, want %q", commands, want)
	}
	if !exists(h.modPath(into)) {
		t.Fatal("a gone file is still placed from the cache")
	}
}

func TestBuildWarnsFromTheRecordedTakedownCheck(t *testing.T) {
	h, into, _ := takedownHarness(t)
	h.takeDown(h.sodium)
	h.mustSync(into, Request{})
	h.env.Modrinth.FiledErr = errors.New("a build asks no provider")

	b, err := h.e.builder(t.Context(), h.project())
	if err != nil {
		t.Fatal(err)
	}
	rep, err := b.Build("client", build.Options{Dir: into})
	if err != nil {
		t.Fatalf("a gone file doesn't block a build: %v", err)
	}
	if n := h.env.Modrinth.Requests["Filed"]; n != 1 {
		t.Fatalf("a build asks no provider, Filed asked %d times", n)
	}
	warnings := rep.SecurityWarnings(h.e.Providers, h.e.usedBy)
	if len(warnings) != 1 || warnings[0].Protection != "takedowns" || !strings.Contains(warnings[0].Message, "sodium: ") || !strings.Contains(warnings[0].Message, "used by survival") {
		t.Fatalf("security warnings: %+v", warnings)
	}
	if st := instance.LoadState(into); st.Takedowns == nil || len(st.Takedowns.Files) != 1 {
		t.Fatalf("a build keeps the recorded check: %+v", st.Takedowns)
	}

	h.remove("sodium")
	b, err = h.e.builder(t.Context(), h.project())
	if err != nil {
		t.Fatal(err)
	}
	if rep, err = b.Build("client", build.Options{Dir: into}); err != nil {
		t.Fatal(err)
	}
	if len(rep.Takedowns) != 0 {
		t.Fatalf("a file the lock no longer holds doesn't warn: %+v", rep.Takedowns)
	}
}

func TestSyncWarnsAboutAFileFiledUnderAnotherProject(t *testing.T) {
	h, into, _ := takedownHarness(t)
	for i := range h.env.Modrinth.Files {
		if h.env.Modrinth.Files[i].ID == h.sodium.ID {
			h.env.Modrinth.Files[i].ProjectID = "OTHER"
		}
	}

	h.mustSync(into, Request{})

	w, warned := takedownWarning(t, h)
	if !warned || !strings.Contains(w.Message, "sodium: Modrinth files the locked version under project OTHER, not AANobbMI") {
		t.Fatalf("a moved file warns: %+v", h.env.Security)
	}
}

func TestSyncOfflineSkipsTheTakedownCheckSilently(t *testing.T) {
	h, into, _ := takedownHarness(t)
	h.takeDown(h.sodium)
	h.env.Modrinth.FiledErr = errors.New("not using the network (--offline)")

	h.mustSync(into, Request{})

	if _, warned := takedownWarning(t, h); warned || h.warned("takedown") {
		t.Fatalf("offline is silent: %v", h.env.Warnings)
	}
	if st := instance.LoadState(into); st.Takedowns != nil {
		t.Fatalf("an offline sync records no check: %+v", st.Takedowns)
	}

	h.env.Modrinth.FiledErr = nil
	h.mustSync(into, Request{})
	if n := h.env.Modrinth.Requests["Filed"]; n != 2 {
		t.Fatalf("the next sync tries again, Filed asked %d times", n)
	}
	if _, warned := takedownWarning(t, h); !warned {
		t.Fatal("the next sync online warns")
	}
}

func TestSyncWithFetchOfflineAsksNoProvider(t *testing.T) {
	h, into, _ := takedownHarness(t)
	h.mustSync(into, Request{Force: true})
	calls := h.env.Modrinth.Requests["Filed"]
	st := instance.LoadState(into)
	st.Takedowns = nil
	if err := instance.WriteState(into, st); err != nil {
		t.Fatal(err)
	}
	h.e.Fetch.Offline = true

	h.mustSync(into, Request{})

	if n := h.env.Modrinth.Requests["Filed"]; n != calls {
		t.Fatalf("an offline sync asks no provider, Filed asked %d times", n-calls)
	}
}
