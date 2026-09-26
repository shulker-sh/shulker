package game

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/env/envtest"
	"shulker.sh/shulker/internal/fetch"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/mojang"
	"shulker.sh/shulker/internal/out"
)

// fillHarness is a store with sources pointed at the fake piston, its progress written to stderr.
type fillHarness struct {
	store   Store
	sources Sources
	stderr  bytes.Buffer
	lock    *lock.Lock
	saved   int
}

func newFillHarness(t *testing.T, p *envtest.Piston, row loader.Loader, remote *loader.Remote) *fillHarness {
	t.Helper()
	f := fetch.New("test")
	piston := mojang.NewPiston(f)
	piston.ManifestURL = p.URL() + "/manifest.json"
	h := &fillHarness{store: Store{Root: t.TempDir(), Resources: p.URL() + "/resources"}, lock: &lock.Lock{Minecraft: "26.2"}}
	if row.Name != "" {
		h.lock.Loader = lock.Loader{Type: row.Name, Version: "1.0.0"}
	}
	printer := &out.Printer{Stdout: &h.stderr, Stderr: &h.stderr}
	h.sources = Sources{
		Fetch:         f,
		Piston:        piston,
		Loader:        row,
		Loaders:       remote,
		InstallerJava: func(context.Context) (string, error) { return "/usr/bin/java", nil },
		SaveLock:      func() error { h.saved++; return nil },
		Log:           printer.Step,
		Progress:      printer.Progress,
	}
	if err := h.store.EnsureProfiles(); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *fillHarness) fill(t *testing.T) Launchable {
	t.Helper()
	l, err := h.store.Fill(context.Background(), h.lock, linux64, h.sources)
	if err != nil {
		t.Fatalf("fill: %v\n%s", err, h.stderr.String())
	}
	return l
}

func TestFillPutsAVanillaVersionAndEverythingItNamesInTheStore(t *testing.T) {
	p := envtest.NewPiston(t)
	h := newFillHarness(t, p, loader.Loader{}, nil)

	l := h.fill(t)

	if l.ID != "26.2" || l.Top.ID != "26.2" || l.Version.MainClass != "net.minecraft.client.main.Main" {
		t.Fatalf("launchable %+v", l)
	}
	if len(l.Assembly.Libraries) != 1 || l.Assembly.Client.Path != "versions/26.2/26.2.jar" {
		t.Fatalf("assembly %+v", l.Assembly)
	}
	icon := envtest.Sha1Hex([]byte(p.Assets["icons/icon_16x16.png"]))
	for _, rel := range []string{
		"versions/26.2/26.2.json",
		"versions/26.2/26.2.jar",
		"libraries/com/mojang/brigadier/1.3.10/brigadier-1.3.10.jar",
		"assets/indexes/26.json",
		"assets/objects/" + icon[:2] + "/" + icon,
	} {
		if _, err := os.Stat(filepath.Join(h.store.Root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("the store is missing %s: %v", rel, err)
		}
	}
	for _, want := range []string{"fetched the minecraft 26.2 version json", "fetched 1 client jar", "fetched 1 library", "fetched 1 asset index", "fetched 2 assets"} {
		if !strings.Contains(h.stderr.String(), want) {
			t.Fatalf("each stage reports itself; %q is missing from\n%s", want, h.stderr.String())
		}
	}
	if _, err := os.Stat(filepath.Join(h.store.Root, "loaders.json")); err == nil {
		t.Fatal("a vanilla version installs no loader")
	}

	before := p.Hits.Load()
	h.fill(t)
	if p.Hits.Load() != before {
		t.Fatalf("a second fill downloaded %d files", p.Hits.Load()-before)
	}
}

func TestFillSavesALoaderProfileAndRemembersIt(t *testing.T) {
	p := envtest.NewPiston(t)
	profile := []byte(`{"id":"fabric-loader-1.0.0-26.2","inheritsFrom":"26.2","mainClass":"net.fabricmc.loader.impl.launch.knot.KnotClient","libraries":[{"name":"net.fabricmc:fabric-loader:1.0.0","downloads":{"artifact":{"path":"net/fabricmc/fabric-loader/1.0.0/fabric-loader-1.0.0.jar","url":"` + p.URL() + `/brigadier.jar","sha1":"` + envtest.Sha1Hex(p.Lib) + `","size":` + fmt.Sprint(len(p.Lib)) + `}}}]}`)
	row := loader.Fake{Name: "fabric", Profile: profile}.Row()
	h := newFillHarness(t, p, row, &loader.Remote{})

	l := h.fill(t)

	if l.ID != "fabric-loader-1.0.0-26.2" || l.Top.InheritsFrom != "26.2" || l.Version.MainClass != "net.fabricmc.loader.impl.launch.knot.KnotClient" {
		t.Fatalf("launchable %+v", l)
	}
	if len(l.Assembly.Libraries) != 2 || l.Assembly.Client.Path != "versions/26.2/26.2.jar" {
		t.Fatalf("the loader's library joins vanilla's and shares its client jar: %+v", l.Assembly)
	}
	if id, ok := h.store.InstalledLoader("fabric-1.0.0-26.2"); !ok || id != l.ID {
		t.Fatalf("the store remembers the loader's version id: %q %v", id, ok)
	}
	if !strings.Contains(h.stderr.String(), "fetched fabric loader 1.0.0 for 26.2") {
		t.Fatalf("the profile fetch reports itself:\n%s", h.stderr.String())
	}

	if err := os.Remove(h.store.VersionJSON(l.ID)); err != nil {
		t.Fatal(err)
	}
	h.sources.Loader = loader.Fake{Name: "fabric"}.Row()
	if _, err := h.store.Fill(context.Background(), h.lock, linux64, h.sources); err == nil {
		t.Fatal("a remembered id whose version json is gone is fetched again, not trusted")
	}
}

func TestFillRunsALoaderInstallerOnceWithTheClientJarInPlace(t *testing.T) {
	p := envtest.NewPiston(t)
	installer := []byte("installer jar")
	mux := http.NewServeMux()
	mux.HandleFunc("/installer.jar", func(w http.ResponseWriter, r *http.Request) { w.Write(installer) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	row := loader.Fake{Name: "neoforge", Installer: srv.URL + "/installer.jar"}.Row()
	var runs []string
	remote := &loader.Remote{Fetch: fetch.New("test"), Cache: &cache.Cache{Dir: t.TempDir()}}
	remote.RunInstaller = func(_ context.Context, java, jar string, args []string) error {
		runs = append(runs, java)
		if got, err := os.ReadFile(jar); err != nil || !bytes.Equal(got, installer) {
			return fmt.Errorf("the installer jar at %s is not the locked one (%v)", jar, err)
		}
		if len(args) != 2 || args[0] != row.InstallClientFlag {
			return fmt.Errorf("installer args %v", args)
		}
		dir := args[1]
		if _, err := os.Stat(filepath.Join(dir, "versions", "26.2", "26.2.jar")); err != nil {
			return fmt.Errorf("the vanilla client jar is not in place for the installer: %v", err)
		}
		m := Store{Root: dir}
		id, err := m.SaveVersion([]byte(`{"id":"neoforge-1.0.0","inheritsFrom":"26.2","mainClass":"cpw.mods.bootstraplauncher.BootstrapLauncher","libraries":[{"name":"net.neoforged:neoforge:1.0.0","downloads":{"artifact":{"path":"net/neoforged/neoforge/1.0.0/neoforge-1.0.0-universal.jar","url":"","sha1":"","size":0}}}]}`))
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Join(dir, "libraries", "net", "neoforged", "neoforge", "1.0.0"), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "libraries", "net", "neoforged", "neoforge", "1.0.0", "neoforge-1.0.0-universal.jar"), []byte("universal"), 0o644); err != nil {
			return err
		}
		profiles := map[string]any{"profiles": map[string]any{"neoforge": map[string]string{"name": "neoforge", "type": "custom", "lastVersionId": id}}}
		data, _ := json.Marshal(profiles)
		return os.WriteFile(filepath.Join(dir, "launcher_profiles.json"), data, 0o644)
	}
	h := newFillHarness(t, p, row, remote)

	l := h.fill(t)

	if l.ID != "neoforge-1.0.0" || l.Top.InheritsFrom != "26.2" {
		t.Fatalf("launchable %+v", l)
	}
	if len(runs) != 1 || runs[0] != "/usr/bin/java" {
		t.Fatalf("the installer ran with the java the sources give: %v", runs)
	}
	if h.lock.Loader.Client == nil || h.saved != 1 {
		t.Fatalf("locking the installer jar saves the lock: client %+v, saved %d", h.lock.Loader.Client, h.saved)
	}
	if len(l.Assembly.Libraries) != 2 {
		t.Fatalf("the installer's library is on the classpath: %+v", l.Assembly.Libraries)
	}

	h.fill(t)
	if len(runs) != 1 || h.saved != 1 {
		t.Fatalf("a second fill runs the installer %d times and saves the lock %d times", len(runs), h.saved)
	}
}
