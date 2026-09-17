package instance

import (
	"os"
	"testing"
)

func TestLaunchKeepMirrorsTheHistoryCount(t *testing.T) {
	if got := (Settings{}).LaunchKeep(); got != DefaultLaunchHistory {
		t.Fatalf("an omitted cap keeps %d, got %d", DefaultLaunchHistory, got)
	}
	for _, want := range []int{-1, 0, 3} {
		if got := (Settings{LaunchHistory: &want}).LaunchKeep(); got != want {
			t.Fatalf("cap %d read as %d", want, got)
		}
	}
}

func TestSaveLaunchesKeepsTheNewest(t *testing.T) {
	dir := t.TempDir()
	records := []Launch{{StartedAt: "1"}, {StartedAt: "2"}, {StartedAt: "3"}, {StartedAt: "4"}}
	if err := SaveLaunches(dir, records, 2); err != nil {
		t.Fatal(err)
	}
	got := LoadLaunches(dir)
	if len(got) != 2 || got[0].StartedAt != "3" || got[1].StartedAt != "4" {
		t.Fatalf("the newest two should be kept: %+v", got)
	}
	if err := SaveLaunches(dir, records, -1); err != nil {
		t.Fatal(err)
	}
	if got := LoadLaunches(dir); len(got) != 4 {
		t.Fatalf("-1 keeps every record: %+v", got)
	}
}

func TestSaveLaunchesZeroRemovesTheFile(t *testing.T) {
	dir := t.TempDir()
	if err := SaveLaunches(dir, []Launch{{StartedAt: "1", Outcome: OutcomeCrashed}}, 5); err != nil {
		t.Fatal(err)
	}
	if err := SaveLaunches(dir, []Launch{{StartedAt: "2"}}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(LaunchesPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("a cap of 0 records nothing and leaves no file: %v", err)
	}
	if got := LoadLaunches(dir); got != nil {
		t.Fatalf("no file is no records: %+v", got)
	}
	if err := SaveLaunches(dir, nil, 0); err != nil {
		t.Fatalf("removing an absent file must be no error: %v", err)
	}
}

func TestLoadLaunchesIgnoresRubbish(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir+"/"+Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LaunchesPath(dir), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A hook must never abort a launch over this file, and nothing hand-edits it.
	if got := LoadLaunches(dir); got != nil {
		t.Fatalf("unreadable records read as none: %+v", got)
	}
}
