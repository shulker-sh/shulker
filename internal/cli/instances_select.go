package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

type instanceSelection struct {
	launcher, side string
	all            bool
}

func (i *instanceSelection) register(cmd *cobra.Command, all string) {
	i.registerWith(cmd, all, "only client or server instances")
}

func (i *instanceSelection) registerWith(cmd *cobra.Command, all, side string) {
	cmd.Flags().StringVar(&i.launcher, "launcher", "", "only instances linked in this launcher: "+launcher.NameList())
	cmd.Flags().StringVar(&i.side, "side", "", side)
	if all != "" {
		cmd.Flags().BoolVar(&i.all, "all", false, all)
	}
}

func (i instanceSelection) narrows() bool { return i.launcher != "" || i.side != "" }

func (i instanceSelection) check() error {
	if i.launcher != "" && launcher.Find(i.launcher) == nil {
		return out.Errorf("usage", "--launcher must be %s, not %q", launcher.NameList(), i.launcher)
	}
	if i.side != "" && i.side != "client" && i.side != "server" {
		return out.Errorf("usage", "--side must be client or server, not %q", i.side)
	}
	return nil
}

func (i instanceSelection) admits(e instanceEntry) bool {
	return (i.launcher == "" || e.Launcher == i.launcher) && (i.side == "" || e.Side == i.side)
}

// selectInstances matches query against ids, then against names (case-insensitive), then against
// directories. With no query it returns every instance the filters admit, sorted for display.
func (a *app) selectInstances(query string, s instanceSelection) ([]instanceEntry, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	entries, err := a.loadInstanceEntries()
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		e := out.Errorf("no-instances", "nothing is linked yet")
		e.Help = "`shulker link prism` adds an instance"
		return nil, e
	}
	sortInstanceEntries(entries)
	var pool []instanceEntry
	for _, e := range entries {
		if s.admits(e) {
			pool = append(pool, e)
		}
	}
	matches := pool
	if query != "" {
		matches = nil
		for _, e := range pool {
			if e.ID == query {
				matches = []instanceEntry{e}
				break
			}
			if strings.EqualFold(e.Name, query) {
				matches = append(matches, e)
			}
		}
		if dir, err := filepath.Abs(query); len(matches) == 0 && err == nil {
			if i := findEntry(pool, dir); i >= 0 {
				matches = pool[i : i+1]
			}
		}
	}
	if len(matches) == 0 {
		e := out.Errorf("instance-not-found", "no instance matches %s", describeSelection(query, s))
		e.Candidates, e.Pass, e.Given = instanceCandidates(pool), instanceIDs(pool), query
		if len(pool) == 0 {
			e.Candidates, e.Pass = instanceCandidates(entries), instanceIDs(entries)
		}
		return nil, e
	}
	if len(matches) > 1 && query != "" && !s.all {
		e := out.Errorf("ambiguous-instance", "%d instances match %s", len(matches), describeSelection(query, s))
		e.Help = "name one by its id, or pass --all"
		e.Candidates, e.Pass, e.Given = instanceCandidates(matches), instanceIDs(matches), query
		return nil, e
	}
	return matches, nil
}

func findEntry(entries []instanceEntry, dir string) int {
	dir = filepath.Clean(dir)
	return slices.IndexFunc(entries, func(e instanceEntry) bool { return filepath.Clean(e.Dir) == dir })
}

func describeSelection(query string, s instanceSelection) string {
	var parts []string
	if query != "" {
		parts = append(parts, strconv.Quote(query))
	}
	if s.launcher != "" {
		parts = append(parts, "--launcher "+s.launcher)
	}
	if s.side != "" {
		parts = append(parts, "--side "+s.side)
	}
	return strings.Join(parts, " with ")
}

func instanceCandidates(entries []instanceEntry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		label := e.ID
		if e.Name != "" && !strings.EqualFold(e.Name, e.ID) {
			label += " (" + e.Name + ")"
		}
		names[i] = fmt.Sprintf("%s — %s", label, e.Dir)
		if e.detached {
			names[i] = fmt.Sprintf("%s — detached build, %s", label, e.Dir)
		} else if e.Launcher != "" {
			names[i] = fmt.Sprintf("%s — %s, %s", label, launcher.Title(e.Launcher), e.Dir)
		}
	}
	return names
}

func instanceIDs(entries []instanceEntry) []string {
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.ID
	}
	return ids
}

func instanceDirs(entries []instanceEntry) []string {
	dirs := make([]string, len(entries))
	for i, e := range entries {
		dirs[i] = e.Dir
	}
	return dirs
}

// unlinkTargets is what unlink's argument picks. Inside a project, a launcher name that no instance
// is called picks the project's instances in that launcher, the reverse of `link <launcher>`.
func (a *app) unlinkTargets(query string, s instanceSelection) ([]instanceEntry, error) {
	name := launcherArg(query)
	if name == "" || s.launcher != "" {
		return a.selectOrDetached(query, s)
	}
	registry, err := a.loadInstances()
	if err != nil {
		return nil, err
	}
	for _, in := range registry {
		if in.ID == query || strings.EqualFold(in.Name, query) {
			return a.selectInstances(query, s)
		}
	}
	inLauncher := s
	inLauncher.launcher = name
	entries, inProject, err := a.projectInstances(inLauncher)
	if err != nil {
		return nil, err
	}
	if !inProject {
		return a.selectOrDetached(query, s)
	}
	var matches []instanceEntry
	for _, e := range entries {
		if inLauncher.admits(e) {
			matches = append(matches, e)
		}
	}
	if len(matches) > 1 && !s.all {
		e := out.Errorf("ambiguous-instance", "this project has %d instances in %s", len(matches), launcher.Title(name))
		e.Help = "name one, or pass --all"
		e.Candidates, e.Pass, e.Given = instanceCandidates(matches), instanceIDs(matches), query
		return nil, e
	}
	return matches, nil
}

// selectOrDetached falls back, when no registered instance matches, to a directory the query names
// that holds a detached build: one with a source in its instance file and no registry row.
func (a *app) selectOrDetached(query string, s instanceSelection) ([]instanceEntry, error) {
	entries, err := a.selectInstances(query, s)
	if err == nil || query == "" {
		return entries, err
	}
	if code := out.AsError(err).Code; code != "instance-not-found" && code != "no-instances" {
		return nil, err
	}
	if e, ok := detachedBuild(query); ok && s.admits(e) {
		return []instanceEntry{e}, nil
	}
	return nil, err
}

func detachedBuild(query string) (instanceEntry, bool) {
	dir, err := filepath.Abs(query)
	if err != nil {
		return instanceEntry{}, false
	}
	if _, err := os.Stat(filepath.Join(dir, manifest.FileName)); err == nil {
		return instanceEntry{}, false
	}
	f, err := instance.Load(dir)
	if err != nil || f.Source == "" || f.IsUnlinked {
		return instanceEntry{}, false
	}
	e := inspectInstance(config.Instance{Name: filepath.Base(dir), Dir: dir})
	e.ID = slugID(e.Name)
	e.detached = true
	return e, true
}

func launcherArg(arg string) string {
	arg = strings.ToLower(arg)
	if arg == "vanilla" {
		return "mojang"
	}
	if launcher.Find(arg) != nil {
		return arg
	}
	return ""
}

func instanceAside(t out.Theme, e instanceEntry) string {
	if e.detached {
		return t.Aside("detached build")
	}
	if e.Launcher == "" {
		return ""
	}
	return t.Aside(launcher.Title(e.Launcher))
}

// instanceHeading shows a detached build's directory, since unlink takes one by path.
func instanceHeading(t out.Theme, e instanceEntry) string {
	heading := t.Bold(e.Label()) + " " + t.Grey(e.ID) + " " + t.Cyan(e.Side) + instanceAside(t, e)
	if e.detached {
		heading += " " + t.Link(t.Grey(e.Dir), e.Dir)
	}
	return heading
}

func instancePickLabel(t out.Theme, e instanceEntry) string {
	if e.detached {
		return instanceHeading(t, e)
	}
	return instanceHeading(t, e) + " " + t.Link(t.Grey(e.Dir), e.Dir)
}

func (a *app) pickInstance(entries []instanceEntry) (instanceEntry, error) {
	t := a.printer.ErrTheme
	return pickOne(a, "Sync which one?", entries,
		func(e instanceEntry) string { return e.ID },
		func(e instanceEntry) string { return instancePickLabel(t, e) },
		func() error {
			e := out.Errorf("ambiguous-instance", "pass a source, -i <id>, or --all to choose what to sync")
			e.Candidates, e.Pass, e.Flag = instanceCandidates(entries), instanceIDs(entries), "--instance"
			return e
		})
}

func (a *app) syncInstance(cmd *cobra.Command, e instanceEntry, req syncRequest) (syncResult, error) {
	if l := launcher.Find(e.Launcher); l != nil && l.IsInstanced {
		if _, err := os.Stat(l.InstanceDir(e.Dir)); errors.Is(err, os.ErrNotExist) {
			fail := out.Errorf("instance-missing", "the %s instance %q is gone (%s)", launcher.Title(e.Launcher), e.Label(), l.InstanceDir(e.Dir))
			fail.Help = fmt.Sprintf("`shulker unlink %s` forgets it", e.ID)
			return syncResult{}, fail
		}
	}
	if p, side, ok, err := a.inPlaceProject(e.Dir); err != nil {
		return syncResult{}, err
	} else if ok {
		return a.syncInPlace(cmd, p, side, req)
	}
	a.packs = nil
	src, err := a.openSource(cmd.Context(), e.Source, e.Ref)
	if err != nil {
		return syncResult{}, err
	}
	req.ref, req.side, req.into = e.Ref, e.Side, e.Dir
	req.assumeClient = req.assumeClient || e.AssumesClient
	return a.sync(cmd.Context(), src, req)
}

type syncInstanceResult struct {
	config.Instance
	Detached bool        `json:"detached,omitempty"`
	OK       bool        `json:"ok"`
	Sync     *syncResult `json:"sync,omitempty"`
	Error    *out.Error  `json:"error,omitempty"`
}

func (a *app) syncInstances(cmd *cobra.Command, entries []instanceEntry, req syncRequest) error {
	results, failed := a.syncEach(cmd, entries, req, false)
	if failed > 0 {
		e := out.Errorf("sync-failed", "%d of %d instances failed to sync", failed, len(entries))
		e.Data = results
		return e
	}
	return a.printer.Emit(results, func(*out.Lines) {})
}

// syncEach syncs every entry, printing each as it finishes; after says a result was printed
// above the first one.
func (a *app) syncEach(cmd *cobra.Command, entries []instanceEntry, req syncRequest, after bool) ([]syncInstanceResult, int) {
	lines := a.printer.Out()
	results := []syncInstanceResult{}
	failed := 0
	for i, e := range entries {
		if !a.printer.JSON {
			if i > 0 || after {
				lines.Blank()
			}
			lines.Heading(instanceHeading(lines.T, e))
		}
		r := syncInstanceResult{Instance: e.Instance, Detached: e.detached, OK: true}
		restore := func() {}
		if len(entries) > 1 || after {
			restore = a.scopeWarnings(e.Label())
		}
		res, err := a.syncInstance(cmd, e, req)
		restore()
		if err != nil {
			failed++
			r.OK, r.Error = false, out.AsError(err)
			a.printer.Report(r.Error)
		} else {
			r.Sync = &res
			if !a.printer.JSON {
				res.print(lines)
			}
		}
		results = append(results, r)
	}
	return results, failed
}
