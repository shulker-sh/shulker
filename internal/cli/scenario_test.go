package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/loader"
)

const scenariosDir = "testdata/scenarios"

// scenario is a scenario.json: commands run back to back on one project, each checked through
// its --json output.
type scenario struct {
	Steps []scenarioStep `json:"steps"`
}

type scenarioStep struct {
	Run    []string        `json:"run"`
	Expect json.RawMessage `json:"expect,omitempty"`
	Reject json.RawMessage `json:"reject,omitempty"`
}

func TestScenarios(t *testing.T) {
	entries, err := os.ReadDir(scenariosDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) { replayScenario(t, filepath.Join(scenariosDir, e.Name())) })
	}
}

// replayScenario runs every step of the scenario in dir, returning the harness it ran on.
func replayScenario(t *testing.T, dir string) *harness {
	t.Helper()
	var sc scenario
	readScenarioFile(t, filepath.Join(dir, "scenario.json"), &sc)
	h := newReplayHarness(t, dir)
	for i, step := range sc.Steps {
		if err := h.runStep(t, step); err != nil {
			t.Fatalf("step %d %v: %v", i+1, step.Run, err)
		}
	}
	return h
}

// newReplayHarness is a harness whose every request the scenario's recording answers, with the
// real loader table and the clock at the recording's time.
func newReplayHarness(t *testing.T, dir string) *harness {
	t.Helper()
	rec, err := readRecording(filepath.Join(dir, "responses.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	r, err := newReplay(rec)
	if err != nil {
		t.Fatal(err)
	}
	real := loader.All
	h := newHarness(t)
	loader.All = real
	h.replay = r
	h.now = rec.Recorded
	return h
}

// runStep runs a step and checks its output, failing on any request the recording lacked.
func (h *harness) runStep(t *testing.T, step scenarioStep) error {
	t.Helper()
	_, stdout, stderr := h.run(t, step.Run...)
	if misses := h.replay.takeMisses(); len(misses) > 0 {
		return fmt.Errorf("not in the recording: %s\nstderr: %s", strings.Join(misses, ", "), stderr)
	}
	if err := checkFragments(string(step.Expect), string(step.Reject), stdout); err != nil {
		return fmt.Errorf("%w\nstderr: %s", err, stderr)
	}
	return nil
}

func readScenarioFile(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

// TestScenarioBudget holds the recordings to the size the repo carries: 1 MB a scenario and 8 MB
// for the set.
func TestScenarioBudget(t *testing.T) {
	const perScenario, total = 1 << 20, 8 << 20
	recordings, err := filepath.Glob(filepath.Join(scenariosDir, "*", "responses.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if len(recordings) == 0 {
		t.Fatal("no recordings")
	}
	var sum int64
	for _, path := range recordings {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() > perScenario {
			t.Errorf("%s is %d bytes, over the %d a scenario may take", path, info.Size(), perScenario)
		}
		sum += info.Size()
	}
	if sum > total {
		t.Errorf("the recordings come to %d bytes, over the %d the set may take", sum, total)
	}
}

func TestReplayServesRebuiltJars(t *testing.T) {
	h := replayScenario(t, filepath.Join(scenariosDir, "fabric-add"))
	jar := h.replay.files["https://cdn.modrinth.com/data/AANobbMI/versions/u1OEbNKx/sodium-fabric-0.6.0%2Bmc1.21.1.jar"]
	var l struct {
		Mods map[string]struct {
			Sha512 string `json:"sha512"`
			Size   int64  `json:"size"`
		} `json:"mods"`
	}
	h.readJSON(t, "shulker.lock", &l)
	if got := l.Mods["sodium"]; got.Sha512 != jar.sha512 || got.Size != int64(len(jar.data)) || got.Sha512 == jar.real.Sha512 {
		t.Fatalf("sodium locked as %+v, rebuilt as %s (%d bytes)", got, jar.sha512, len(jar.data))
	}
	if !(&cache.Cache{Dir: h.cache}).Has(jar.sha512) {
		t.Fatal("the rebuilt jar is not in the cache")
	}
}

func TestReplayMissNamesMethodAndPath(t *testing.T) {
	h := newReplayHarness(t, filepath.Join(scenariosDir, "fabric-add"))
	if err := h.runStep(t, scenarioStep{Run: []string{"create", "--fabric", "--minecraft", "1.21.1", "--json"}}); err != nil {
		t.Fatal(err)
	}
	err := h.runStep(t, scenarioStep{Run: []string{"add", "iris", "--json"}, Expect: json.RawMessage(`{"ok":true}`)})
	if err == nil || !strings.Contains(err.Error(), "GET api.modrinth.com/v2/project/iris") {
		t.Fatalf("got %v", err)
	}
}
