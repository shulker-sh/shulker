package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestStateWarningNudges(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "build")
	statePath := filepath.Join(h.dir, "build", "client", ".shulker", "state.json")
	writeState := func(data string) {
		t.Helper()
		if err := os.WriteFile(statePath, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	writeState(`{"$schema": "https://shulker.sh/schema/v2/state.json", "side": "client", "files": {}}`)
	_, stderr := h.mustRunStderr(t, "diff")
	for _, want := range []string{
		"! " + statePath + " was written by a newer shulker (schema v2; this one reads up to v1); treating every file as not written by shulker",
		"Update shulker:",
		"$ shulker self update",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("newer state: stderr lacks %q:\n%s", want, stderr)
		}
	}
	if strings.Contains(stderr, "Take them over") {
		t.Errorf("newer state: nudged to --force:\n%s", stderr)
	}

	writeState(`{"side": "client", "files": {}}`)
	_, stderr = h.mustRunStderr(t, "diff")
	for _, want := range []string{
		"! " + statePath + " is unreadable (names no $schema, which this shulker doesn't know); treating every file as not written by shulker",
		"Take them over:",
		"$ shulker build --force",
	} {
		if !strings.Contains(stderr, want) {
			t.Errorf("unreadable state, diff: stderr lacks %q:\n%s", want, stderr)
		}
	}

	_, _, stderr = h.run(t, "build", "--os", "linux")
	if !strings.Contains(stderr, "$ shulker build --os linux --force") {
		t.Errorf("unreadable state, build: nudge doesn't rerun the build:\n%s", stderr)
	}

	writeState(`{"side": "client", "files": {}}`)
	_, stderr = h.mustRunStderr(t, "build", "--force")
	if !strings.Contains(stderr, "is unreadable") || strings.Contains(stderr, "Take them over") {
		t.Errorf("a forced build still nudges to --force:\n%s", stderr)
	}
}

func TestRerunForced(t *testing.T) {
	root := &cobra.Command{Use: "shulker"}
	root.PersistentFlags().Bool("json", false, "")
	root.PersistentFlags().StringP("instance", "i", "", "")
	var got string
	sync := &cobra.Command{Use: "sync", RunE: func(cmd *cobra.Command, args []string) error {
		got = rerunForced(cmd, args)
		return nil
	}}
	sync.Flags().Bool("force", false, "")
	sync.Flags().String("into", "", "")
	sync.Flags().StringArray("with", nil, "")
	root.AddCommand(sync)
	root.SetArgs([]string{"sync", "../my pack", "--into", "/tmp/a b", "--with", "x", "--with", "y", "--json", "-i", "alpha"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if want := `shulker sync "../my pack" -i alpha --into "/tmp/a b" --with x --with y --force`; got != want {
		t.Errorf("rerunForced = %q, want %q", got, want)
	}
}
