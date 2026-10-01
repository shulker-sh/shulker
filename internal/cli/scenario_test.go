package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/fetch/fetchtest"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/resolve"
)

const (
	scenariosDir = "testdata/scenarios"
	// scenarioBudget is the most a scenario's recording may take, gzipped.
	scenarioBudget = 3 << 19
)

// scenario is a scenario.json: commands run back to back on one project, each checked through
// its --json output.
type scenario struct {
	Steps []scenarioStep `json:"steps"`
}

// scenarioStep runs a command and checks its output, or places manual downloads in the project.
type scenarioStep struct {
	Run      []string        `json:"run,omitempty"`
	Download []string        `json:"download,omitempty"`
	Expect   json.RawMessage `json:"expect,omitempty"`
	Reject   json.RawMessage `json:"reject,omitempty"`
}

var (
	recordFlag  = flag.Bool("record", false, "fill each scenario's recording with what the live services answer to requests it lacks; needs SHULKER_CURSEFORGE_KEY")
	replaceFlag = flag.Bool("replace", false, "with -record, record the scenarios named by -run wholesale")
	liveFlag    = flag.Bool("live", false, "run each scenario against the live services, writing its recording to a temp dir; needs SHULKER_CURSEFORGE_KEY")
)

func TestScenarios(t *testing.T) {
	key := scenarioKey(t)
	entries, err := os.ReadDir(scenariosDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			dir := filepath.Join(scenariosDir, e.Name())
			switch {
			case *liveFlag:
				out, err := os.MkdirTemp("", "shulker-scenario-"+e.Name()+"-")
				if err != nil {
					t.Fatal(err)
				}
				recordScenario(t, dir, out, key, true)
			case *recordFlag:
				recordScenario(t, dir, dir, key, *replaceFlag)
			default:
				replayScenario(t, dir, filepath.Join(dir, "responses.json.gz"))
			}
		})
	}
}

// scenarioKey checks the recording flags and returns the CurseForge key a recording or live run
// needs, failing before any scenario starts without one.
func scenarioKey(t *testing.T) string {
	t.Helper()
	switch {
	case *replaceFlag && !*recordFlag:
		t.Fatal("-replace goes with -record")
	case *recordFlag && *liveFlag:
		t.Fatal("-record and -live can't go together: -live leaves the recordings alone")
	case !*recordFlag && !*liveFlag:
		return ""
	}
	if *replaceFlag {
		test, name, _ := strings.Cut(flag.Lookup("test.run").Value.String(), "/")
		if !strings.Contains(test, "TestScenarios") || strings.Trim(name, "^$") == "" {
			t.Fatal("-replace records a scenario wholesale, so name it: -run 'TestScenarios/<name>'")
		}
	}
	key := os.Getenv("SHULKER_CURSEFORGE_KEY")
	if key == "" {
		t.Fatal("recording reaches CurseForge, so it needs SHULKER_CURSEFORGE_KEY")
	}
	return key
}

// recordScenario runs the scenario in dir against its recording, fetching live whatever the
// recording lacks, or everything when fresh, and writes the recording to outDir even when a step
// fails. It then replays what it wrote, which proves the recording complete.
func recordScenario(t *testing.T, dir, outDir, key string, fresh bool) {
	t.Helper()
	var sc scenario
	readScenarioFile(t, filepath.Join(dir, "scenario.json"), &sc)
	rec := &recording{Recorded: time.Now().UTC().Truncate(time.Second)}
	if !fresh {
		existing, err := readRecording(filepath.Join(dir, "responses.json.gz"))
		switch {
		case err == nil:
			rec = existing
		case !errors.Is(err, fs.ErrNotExist):
			t.Fatal(err)
		}
	}
	r, err := newReplay(rec)
	if err != nil {
		t.Fatal(err)
	}
	r.live = liveClient()
	h := newScenarioHarness(t, r, rec.Recorded)
	h.curseForgeKey = key
	failed := runSteps(t, h, sc)
	out := filepath.Join(outDir, "responses.json.gz")
	if err := writeRecording(out, r.rec, key); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", out)
	if err := withinBudget(out, r.rec); err != nil {
		t.Error(err)
	}
	if failed != nil {
		t.Fatal(failed)
	}
	replayScenario(t, dir, out)
}

// withinBudget fails a recording over the 1.5 MB a scenario may take, naming its largest responses.
func withinBudget(path string, rec *recording) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() <= scenarioBudget {
		return nil
	}
	type entry struct {
		url  string
		size int
	}
	var entries []entry
	for _, ex := range rec.Exchanges {
		entries = append(entries, entry{ex.Method + " " + ex.URL, len(ex.Body) + len(ex.BodyBytes)})
	}
	for _, f := range rec.Files {
		data, _ := json.Marshal(f.Jar)
		entries = append(entries, entry{"file " + f.URL, len(data)})
	}
	slices.SortFunc(entries, func(a, b entry) int { return b.size - a.size })
	var largest []string
	for _, e := range entries[:min(10, len(entries))] {
		largest = append(largest, fmt.Sprintf("  %d bytes  %s", e.size, e.url))
	}
	return fmt.Errorf("%s is %d bytes gzipped, over the %d a scenario may take; the largest, before gzip:\n%s", path, info.Size(), scenarioBudget, strings.Join(largest, "\n"))
}

// replayScenario runs every step of the scenario in dir against the recording at path, returning
// the harness it ran on.
func replayScenario(t *testing.T, dir, path string) *harness {
	t.Helper()
	var sc scenario
	readScenarioFile(t, filepath.Join(dir, "scenario.json"), &sc)
	h := newReplayHarness(t, path)
	if err := runSteps(t, h, sc); err != nil {
		t.Fatal(err)
	}
	return h
}

// runSteps runs the scenario's steps in order, stopping at the first that fails.
func runSteps(t *testing.T, h *harness, sc scenario) error {
	t.Helper()
	for i, step := range sc.Steps {
		if err := h.runStep(t, step); err != nil {
			return fmt.Errorf("step %d %v: %w", i+1, step.Run, err)
		}
	}
	return nil
}

// newReplayHarness is a harness whose every request the recording at path answers.
func newReplayHarness(t *testing.T, path string) *harness {
	t.Helper()
	rec, err := readRecording(path)
	if err != nil {
		t.Fatal(err)
	}
	r, err := newReplay(rec)
	if err != nil {
		t.Fatal(err)
	}
	return newScenarioHarness(t, r, rec.Recorded)
}

// newScenarioHarness is a harness r answers every request for, with the real loader table and the
// clock at now.
func newScenarioHarness(t *testing.T, r *replay, now time.Time) *harness {
	t.Helper()
	real := loader.All
	h := newHarness(t)
	loader.All = real
	h.replay = r
	h.now = now
	return h
}

// runStep runs a step and checks its output, failing on any request the recording lacked.
func (h *harness) runStep(t *testing.T, step scenarioStep) error {
	t.Helper()
	if len(step.Download) > 0 {
		return h.placeDownloads(step.Download)
	}
	_, stdout, stderr := h.run(t, stepArgs(h.dir, step.Run)...)
	if h.replay.live != nil {
		t.Logf("%v:\n%s", step.Run, stdout)
	}
	if misses := h.replay.takeMisses(); len(misses) > 0 {
		return fmt.Errorf("not in the recording: %s\nstderr: %s", strings.Join(misses, ", "), stderr)
	}
	if err := checkFragments(string(step.Expect), string(step.Reject), stdout); err != nil {
		return fmt.Errorf("%w\nstderr: %s", err, stderr)
	}
	return nil
}

// stepArgs are a step's arguments with a relative -C or --dir read inside the project folder dir,
// where the test's own working directory would put it in the repo.
func stepArgs(dir string, args []string) []string {
	args = slices.Clone(args)
	for i := 1; i < len(args); i++ {
		if (args[i-1] == "-C" || args[i-1] == "--dir") && !filepath.IsAbs(args[i]) {
			args[i] = filepath.Join(dir, args[i])
		}
	}
	return args
}

// placeDownloads puts each file in the project's downloads/ under its URL's file name, fetched
// through the replay as the CLI would fetch it.
func (h *harness) placeDownloads(urls []string) error {
	client := fetchtest.Everything(h.server)
	dir := filepath.Join(h.dir, resolve.DownloadsDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, raw := range urls {
		u, err := url.Parse(raw)
		if err != nil {
			return err
		}
		resp, err := client.Get(raw)
		if err != nil {
			return err
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return err
		}
		if misses := h.replay.takeMisses(); len(misses) > 0 {
			return fmt.Errorf("not in the recording: %s", strings.Join(misses, ", "))
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("%s: HTTP %d", raw, resp.StatusCode)
		}
		if err := os.WriteFile(filepath.Join(dir, path.Base(u.Path)), data, 0o644); err != nil {
			return err
		}
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

// TestScenarioBudget holds the recordings to the size the repo carries: 1.5 MB a scenario and 8 MB
// for the set.
func TestScenarioBudget(t *testing.T) {
	const total = 8 << 20
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
		if info.Size() > scenarioBudget {
			t.Errorf("%s is %d bytes, over the %d a scenario may take", path, info.Size(), scenarioBudget)
		}
		sum += info.Size()
	}
	if sum > total {
		t.Errorf("the recordings come to %d bytes, over the %d the set may take", sum, total)
	}
}

func TestReplayServesRebuiltJars(t *testing.T) {
	dir := filepath.Join(scenariosDir, "fabric-add")
	h := replayScenario(t, dir, filepath.Join(dir, "responses.json.gz"))
	jar := h.replay.files[sodiumURL(t, h.replay)]
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
	h := newReplayHarness(t, filepath.Join(scenariosDir, "fabric-add", "responses.json.gz"))
	if err := h.runStep(t, scenarioStep{Run: []string{"create", "--fabric", "--minecraft", "1.21.1", "--json"}}); err != nil {
		t.Fatal(err)
	}
	err := h.runStep(t, scenarioStep{Run: []string{"add", "iris", "--json"}, Expect: json.RawMessage(`{"ok":true}`)})
	if err == nil || !strings.Contains(err.Error(), "GET api.modrinth.com/v2/project/iris") {
		t.Fatalf("got %v", err)
	}
}

func TestDownloadStepPlacesTheRebuiltFile(t *testing.T) {
	h := newReplayHarness(t, filepath.Join(scenariosDir, "fabric-add", "responses.json.gz"))
	u := sodiumURL(t, h.replay)
	if err := h.runStep(t, scenarioStep{Download: []string{u}}); err != nil {
		t.Fatal(err)
	}
	name, _ := url.PathUnescape(path.Base(u))
	data, err := os.ReadFile(filepath.Join(h.dir, resolve.DownloadsDir, name))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, h.replay.files[u].data) {
		t.Fatal("the placed file is not the rebuilt jar")
	}
	err = h.runStep(t, scenarioStep{Download: []string{"https://cdn.modrinth.com/data/missing.jar"}})
	if err == nil || !strings.Contains(err.Error(), "GET cdn.modrinth.com/data/missing.jar") {
		t.Fatalf("got %v", err)
	}
}

// sodiumURL is where fabric-add's recording downloads Sodium from.
func sodiumURL(t *testing.T, r *replay) string {
	t.Helper()
	for _, f := range r.rec.Files {
		if strings.Contains(f.URL, "/sodium-fabric-") {
			return f.URL
		}
	}
	t.Fatal("fabric-add downloads no Sodium jar")
	return ""
}

func TestAStepsRelativeDirIsInsideTheProject(t *testing.T) {
	got := stepArgs("/p", []string{"create", "-C", "quilt", "--dir", "a/b", "--name", "x", "-C", "/abs"})
	want := []string{"create", "-C", "/p/quilt", "--dir", "/p/a/b", "--name", "x", "-C", "/abs"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
