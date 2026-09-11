package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/out"
)

type linkSelection struct {
	launcher, side string
	all            bool
}

func (s *linkSelection) register(cmd *cobra.Command, all string) {
	cmd.Flags().StringVar(&s.launcher, "launcher", "", "only entries linked in this launcher: prism, multimc, or mojang")
	cmd.Flags().StringVar(&s.side, "side", "", "only client or server entries")
	if all != "" {
		cmd.Flags().BoolVar(&s.all, "all", false, all)
	}
}

func (s linkSelection) narrows() bool { return s.launcher != "" || s.side != "" }

func (s linkSelection) check() error {
	if s.launcher != "" && !slices.Contains(launcherOrder, s.launcher) {
		return out.Errorf("usage", "--launcher must be prism, multimc, or mojang, not %q", s.launcher)
	}
	if s.side != "" && s.side != "client" && s.side != "server" {
		return out.Errorf("usage", "--side must be client or server, not %q", s.side)
	}
	return nil
}

func (s linkSelection) admits(l config.Link) bool {
	return (s.launcher == "" || l.Launcher == s.launcher) && (s.side == "" || l.Side == s.side)
}

// selectLinks matches query against entry names (case-insensitive), then against entry
// directories. With no query it returns every entry the filters admit, sorted for display.
func (a *app) selectLinks(query string, s linkSelection) ([]config.Link, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	links, err := a.loadLinks()
	if err != nil {
		return nil, err
	}
	if len(links) == 0 {
		return nil, out.Errorf("no-links", "nothing is linked yet; `shulker link prism` or `shulker sync --into <dir>` adds an entry")
	}
	slices.SortStableFunc(links, compareLinks)
	var pool []config.Link
	for _, l := range links {
		if s.admits(l) {
			pool = append(pool, l)
		}
	}
	matches := pool
	if query != "" {
		matches = nil
		for _, l := range pool {
			if strings.EqualFold(l.Name, query) {
				matches = append(matches, l)
			}
		}
		if dir, err := filepath.Abs(query); len(matches) == 0 && err == nil {
			if i, ok := config.FindLink(pool, dir); ok {
				matches = pool[i : i+1]
			}
		}
	}
	if len(matches) == 0 {
		e := out.Errorf("instance-not-found", "no linked instance or synced directory matches %s", describeSelection(query, s))
		e.Candidates = linkCandidates(pool)
		if len(pool) == 0 {
			e.Candidates = linkCandidates(links)
		}
		return nil, e
	}
	if len(matches) > 1 && query != "" && !s.all {
		e := out.Errorf("ambiguous-instance", "%d entries match %s; narrow it with --launcher, --side, or the directory, or pass --all", len(matches), describeSelection(query, s))
		e.Candidates = linkCandidates(matches)
		return nil, e
	}
	return matches, nil
}

func describeSelection(query string, s linkSelection) string {
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

func linkCandidates(links []config.Link) []string {
	names := make([]string, len(links))
	for i, l := range links {
		names[i] = fmt.Sprintf("%s (%s)", l.Name, l.Dir)
		if l.Launcher != "" {
			names[i] = fmt.Sprintf("%s (%s, %s)", l.Name, l.Launcher, l.Dir)
		}
	}
	return names
}

func linkHeading(l config.Link) string {
	if l.Launcher == "" {
		return fmt.Sprintf("%s (%s)", l.Name, l.Side)
	}
	return fmt.Sprintf("%s (%s, %s)", l.Name, l.Side, launcherTitle(l.Launcher))
}

func (a *app) pickLink(links []config.Link) (config.Link, error) {
	if a.printer.JSON || a.tty == nil || !a.tty() {
		e := out.Errorf("ambiguous-instance", "pass a source, --instance <name>, or --all to choose what to sync")
		e.Candidates = linkCandidates(links)
		return config.Link{}, e
	}
	w := a.printer.Stderr
	for i, l := range links {
		fmt.Fprintf(w, "%3d) %s  %s\n", i+1, linkHeading(l), l.Dir)
	}
	fmt.Fprintf(w, "Sync which one? [1-%d] ", len(links))
	line, _ := bufio.NewReader(a.stdin).ReadString('\n')
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(links) {
		return config.Link{}, out.Errorf("usage", "pick a number from 1 to %d", len(links))
	}
	return links[n-1], nil
}

func (a *app) syncLink(cmd *cobra.Command, l config.Link, req syncRequest) (syncResult, error) {
	if l.Launcher == "prism" || l.Launcher == "multimc" {
		if _, err := os.Stat(filepath.Dir(l.Dir)); errors.Is(err, os.ErrNotExist) {
			return syncResult{}, out.Errorf("instance-missing", "the %s instance %q is gone (%s); `shulker unlink %s` forgets it", launcherTitle(l.Launcher), l.Name, filepath.Dir(l.Dir), launcher.CommandArg(l.Name))
		}
	}
	a.packs = nil
	src, err := a.openSource(cmd.Context(), l.Source, l.Ref)
	if err != nil {
		return syncResult{}, err
	}
	req.ref, req.target, req.into = l.Ref, l.Target, l.Dir
	return a.sync(cmd.Context(), src, req)
}

type syncLinkResult struct {
	config.Link
	OK    bool        `json:"ok"`
	Sync  *syncResult `json:"sync,omitempty"`
	Error *out.Error  `json:"error,omitempty"`
}

func (a *app) syncLinks(cmd *cobra.Command, links []config.Link, req syncRequest) error {
	w := a.printer.Stdout
	results := []syncLinkResult{}
	failed := 0
	for i, l := range links {
		if !a.printer.JSON {
			if i > 0 {
				fmt.Fprintln(w)
			}
			fmt.Fprintln(w, linkHeading(l))
		}
		r := syncLinkResult{Link: l, OK: true}
		restore := func() {}
		if len(links) > 1 {
			restore = a.scopeWarnings(l.Name)
		}
		res, err := a.syncLink(cmd, l, req)
		restore()
		if err != nil {
			failed++
			r.OK, r.Error = false, out.AsError(err)
			a.progress("error: %s", r.Error.Message)
		} else {
			r.Sync = &res
			if !a.printer.JSON {
				res.print(w)
			}
		}
		results = append(results, r)
	}
	if failed > 0 {
		e := out.Errorf("sync-failed", "%d of %d entries failed to sync", failed, len(links))
		e.Data = results
		return e
	}
	return a.printer.Emit(results, func(io.Writer) {})
}
