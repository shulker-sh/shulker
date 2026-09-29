package build

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

const (
	HistoryDir    = "history"
	historyMeta   = "entry.json"
	historyConfig = "config"
	historyStamp  = "20060102-150405"
)

// HistoryEntry is one restorable state: the manifest, the lock and the whole
// config folder as they were before a build changed them.
type HistoryEntry struct {
	ID            string `json:"id"`
	TakenAt       string `json:"takenAt"`
	Side          string `json:"side"`
	Reason        string `json:"reason"`
	Minecraft     string `json:"minecraft,omitempty"`
	Loader        string `json:"loader,omitempty"`
	Mods          int    `json:"mods"`
	ResourcePacks int    `json:"resourcepacks,omitempty"`
	Shaders       int    `json:"shaders,omitempty"`
	Datapacks     int    `json:"datapacks,omitempty"`
	Written       int    `json:"written,omitempty"`
	Removed       int    `json:"removed,omitempty"`
}

func (b *Builder) takeHistory(dir, side, reason string, written, removed int, report *Report) error {
	keep := b.Manifest.HistoryKeep()
	e, err := TakeHistory(dir, keep, HistoryEntry{
		Side:    side,
		Reason:  reason,
		Written: written,
		Removed: removed,
	})
	if err != nil || e.ID == "" {
		return err
	}
	report.History = e.ID
	warning, err := HistoryWarning(dir, keep)
	if err != nil || warning == "" {
		return err
	}
	report.Warnings = append(report.Warnings, warning)
	return nil
}

// planDrift reports whether a build would touch anything the player changed,
// which is the only reason a build takes an entry of its own: a relock has
// already kept whatever it was about to rewrite.
func planDrift(plans []planned) bool {
	for _, f := range plans {
		if f.isForced {
			return true
		}
		switch f.state {
		case stateKept, stateConflict, stateUntracked, stateOrphan:
			return true
		}
		if len(f.merge.kept) > 0 || len(f.merge.overrode) > 0 {
			return true
		}
	}
	return false
}

// HistoryWarning is the line to print when entries have passed the number the
// manifest keeps. Nothing but `history prune` removes them.
func HistoryWarning(dir string, keep int) (string, error) {
	if keep <= 0 {
		return "", nil
	}
	entries, err := History(dir)
	if err != nil || len(entries) <= keep {
		return "", err
	}
	return fmt.Sprintf("%d history entries are kept; `shulker history prune` trims them to %d.", len(entries), keep), nil
}

func HistoryPath(dir string) string {
	return filepath.Join(dir, instance.Dir, HistoryDir)
}

func historyEntryPath(dir, id string) string {
	return filepath.Join(HistoryPath(dir), id)
}

// TakeHistory copies the project's manifest, lock and config folder into a new
// entry. It returns the entry it wrote, or a zero entry when history is off.
func TakeHistory(dir string, keep int, e HistoryEntry) (HistoryEntry, error) {
	if keep == 0 {
		return HistoryEntry{}, nil
	}
	now := time.Now()
	e.ID = now.Format(historyStamp)
	e.TakenAt = now.UTC().Format(time.RFC3339)
	for n := 2; ; n++ {
		if _, err := os.Stat(historyEntryPath(dir, e.ID)); errors.Is(err, fs.ErrNotExist) {
			break
		} else if err != nil {
			return HistoryEntry{}, err
		}
		e.ID = now.Format(historyStamp) + "-" + strconv.Itoa(n)
	}
	into := historyEntryPath(dir, e.ID)
	if err := os.MkdirAll(into, 0o755); err != nil {
		return HistoryEntry{}, err
	}
	// The lock on disk is the state being snapshotted, whatever the caller is
	// about to write, and it says which placed files the cache can produce again.
	lk, lockErr := lock.Load(filepath.Join(dir, lock.FileName))
	if lockErr == nil {
		e.Minecraft, e.Loader, e.Mods = lk.Minecraft, lk.Loader.Type, len(lk.Mods)
		e.ResourcePacks, e.Shaders, e.Datapacks = len(lk.ResourcePacks), len(lk.Shaders), len(lk.Datapacks)
	}
	for _, name := range []string{manifest.FileName, lock.FileName} {
		if err := copyFile(filepath.Join(dir, name), filepath.Join(into, name)); err != nil {
			return HistoryEntry{}, err
		}
	}
	if err := copyFile(instance.StatePath(dir), filepath.Join(into, instance.StateFile)); err != nil {
		return HistoryEntry{}, err
	}
	cached := leftToCache(dir, lk)
	if err := copyTree(dir, historyConfig, into, cached); err != nil {
		return HistoryEntry{}, err
	}
	for _, rel := range managedFiles(dir, cached) {
		from := filepath.Join(dir, filepath.FromSlash(rel))
		if err := copyFile(from, filepath.Join(into, filepath.FromSlash(rel))); err != nil {
			return HistoryEntry{}, err
		}
	}
	if err := fsutil.WriteJSON(filepath.Join(into, historyMeta), e); err != nil {
		return HistoryEntry{}, err
	}
	return e, nil
}

// managedFiles are the files the last build wrote that an entry has to copy:
// everything except the config folder, copied whole separately, and except what
// the cache can place again from the lock.
func managedFiles(dir string, cached func(rel string) bool) []string {
	var rels []string
	for rel := range instance.LoadState(dir).Files {
		if cached(rel) || rel == historyConfig || strings.HasPrefix(rel, historyConfig+"/") {
			continue
		}
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	return rels
}

// leftToCache reports a placed file the cache can place again from the lock, which an entry
// leaves out. A datapack's folder hangs on the side, its global datapack mods and the server's
// level-name, so it is matched by its file name in any folder a build may place one in.
func leftToCache(dir string, lk *lock.Lock) func(rel string) bool {
	placed := cachedPlacements(lk)
	datapacks := map[string]bool{}
	if lk != nil {
		for _, p := range lk.Datapacks {
			datapacks[p.Filename] = true
		}
	}
	level := "world"
	if data, err := os.ReadFile(filepath.Join(dir, PropertiesFile)); err == nil {
		level = levelName(parseProperties(data))
	}
	folders := append(slices.Clone(lock.DatapackFolders), level+"/datapacks")
	return func(rel string) bool {
		return placed[rel] || datapacks[path.Base(rel)] && slices.Contains(folders, path.Dir(rel))
	}
}

func cachedPlacements(lk *lock.Lock) map[string]bool {
	placed := map[string]bool{VanillaServerFile: true}
	if lk == nil {
		return placed
	}
	for _, m := range lk.Mods {
		placed["mods/"+m.Filename] = true
	}
	for _, p := range lk.ResourcePacks {
		placed[p.Path(manifest.TypeResourcePack)] = true
	}
	for _, s := range lk.Shaders {
		placed[s.Path(manifest.TypeShader)] = true
	}
	return placed
}

// History lists the entries in a directory, newest first.
func History(dir string) ([]HistoryEntry, error) {
	names, err := os.ReadDir(HistoryPath(dir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entries := []HistoryEntry{}
	for _, name := range names {
		if !name.IsDir() {
			continue
		}
		e, err := readHistoryEntry(dir, name.Name())
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID > entries[j].ID })
	return entries, nil
}

func readHistoryEntry(dir, id string) (HistoryEntry, error) {
	e := HistoryEntry{ID: id}
	data, err := os.ReadFile(filepath.Join(historyEntryPath(dir, id), historyMeta))
	if errors.Is(err, fs.ErrNotExist) {
		return e, nil
	}
	if err != nil {
		return e, err
	}
	if err := json.Unmarshal(data, &e); err != nil {
		e := out.Errorf("history-invalid", "history entry %s is unreadable", id)
		e.WithCause("json", err)
		e.Help = "delete " + historyEntryPath(dir, id) + " to drop it"
		return HistoryEntry{}, e
	}
	e.ID = id
	return e, nil
}

// HistoryChange is what restoring an entry would do to one key: From is what is locked now and
// To what the entry holds, so one of them empty is a drop or an add back.
type HistoryChange struct {
	Mod  string `json:"mod"`
	Kind string `json:"kind,omitempty"`
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
}

// HistoryChanges compares what an entry's lock holds with now, the lock as it stands, across mods
// and each pack kind, keys sorted, so From is what is installed and To is what restoring would put
// back.
func HistoryChanges(dir string, now *lock.Lock, e HistoryEntry) ([]HistoryChange, error) {
	entry := historyEntryPath(dir, e.ID)
	was, err := lock.Load(filepath.Join(entry, lock.FileName))
	if errors.Is(err, fs.ErrNotExist) {
		fail := out.Errorf("history-invalid", "history entry %s has no lock", e.ID)
		fail.Help = fmt.Sprintf("delete %s to drop it", entry)
		return nil, fail
	}
	if err != nil {
		invalid := out.Errorf("history-invalid", "history entry %s has an unreadable lock", e.ID)
		invalid.Help = fmt.Sprintf("delete %s to drop it", entry)
		invalid.Rows = []out.Detail{{Label: "lock", Text: out.AsError(err).Message}}
		return nil, invalid
	}
	type section struct {
		kind string
		now  map[string]string
		was  map[string]string
	}
	sections := []section{{"", modVersions(now.Mods), modVersions(was.Mods)}}
	for _, kind := range manifest.PackKinds {
		sections = append(sections, section{kind, packVersions(now.Packs(kind)), packVersions(was.Packs(kind))})
	}
	changes := []HistoryChange{}
	for _, s := range sections {
		keys := []string{}
		for key := range s.now {
			keys = append(keys, key)
		}
		for key := range s.was {
			if _, both := s.now[key]; !both {
				keys = append(keys, key)
			}
		}
		slices.Sort(keys)
		for _, key := range keys {
			now, installed := s.now[key]
			then, kept := s.was[key]
			switch {
			case installed && !kept:
				changes = append(changes, HistoryChange{Mod: key, Kind: s.kind, From: now})
			case !installed && kept:
				changes = append(changes, HistoryChange{Mod: key, Kind: s.kind, To: then})
			case now != then:
				changes = append(changes, HistoryChange{Mod: key, Kind: s.kind, From: now, To: then})
			}
		}
	}
	return changes, nil
}

func modVersions(mods map[string]lock.Mod) map[string]string {
	versions := make(map[string]string, len(mods))
	for key, m := range mods {
		versions[key] = m.VersionNumber
	}
	return versions
}

func packVersions(packs map[string]lock.Pack) map[string]string {
	versions := make(map[string]string, len(packs))
	for key, p := range packs {
		versions[key] = p.VersionNumber
	}
	return versions
}

// PickHistory takes the nth newest entry, 1 being the newest.
func PickHistory(dir string, n int) (HistoryEntry, error) {
	entries, err := History(dir)
	if err != nil {
		return HistoryEntry{}, err
	}
	if len(entries) == 0 {
		e := out.Errorf("history-empty", "%s has no history entries yet", dir)
		e.Help = "one is taken before a build changes anything"
		return HistoryEntry{}, e
	}
	if n < 1 || n > len(entries) {
		e := out.Errorf("history-missing", "there is no history entry %d", n)
		e.Rows = []out.Detail{{Label: "kept", Text: strconv.Itoa(len(entries))}}
		return HistoryEntry{}, e
	}
	return entries[n-1], nil
}

// RestoreHistory puts everything an entry holds back where it came from. The
// config folder is replaced whole; the mods and datapacks themselves come back from
// the cache when the restored lock is built.
func RestoreHistory(dir string, e HistoryEntry) error {
	from := historyEntryPath(dir, e.ID)
	if err := os.RemoveAll(filepath.Join(dir, historyConfig)); err != nil {
		return err
	}
	return filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, relErr := filepath.Rel(from, path)
		if relErr != nil {
			return relErr
		}
		switch rel {
		case historyMeta:
			return nil
		case instance.StateFile:
			return copyFile(path, instance.StatePath(dir))
		}
		return copyFile(path, filepath.Join(dir, rel))
	})
}

// PruneHistory removes every entry but the newest keep, and reports what went.
// A negative keep removes nothing.
func PruneHistory(dir string, keep int) ([]HistoryEntry, error) {
	if keep < 0 {
		return nil, nil
	}
	entries, err := History(dir)
	if err != nil || len(entries) <= keep {
		return nil, err
	}
	dropped := entries[keep:]
	for _, e := range dropped {
		if err := os.RemoveAll(historyEntryPath(dir, e.ID)); err != nil {
			return nil, err
		}
	}
	return dropped, nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return fsutil.Write(dst, data)
}

// copyTree copies the folder rel in dir into the same place under into, leaving out what skip
// reports, by its path relative to dir.
func copyTree(dir, rel, into string, skip func(rel string) bool) error {
	src := filepath.Join(dir, rel)
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) && path == src {
				return nil
			}
			return err
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(into, rel), 0o755)
		}
		if !d.Type().IsRegular() || skip(filepath.ToSlash(rel)) {
			return nil
		}
		return copyFile(path, filepath.Join(into, rel))
	})
}
