package cli

import (
	"archive/zip"
	"bytes"
	"cmp"
	"compress/gzip"
	"crypto/sha512"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"shulker.sh/shulker/internal/fetch/fetchtest"
)

// keptFiles are the files a recording keeps from a zip: every loader's mod metadata, the manifest
// an FML library is known by, a resource pack's and a modpack's own description, and the version
// a server jar carries.
var keptFiles = []string{
	"fabric.mod.json", "quilt.mod.json", "META-INF/mods.toml", "META-INF/neoforge.mods.toml", "mcmod.info",
	"META-INF/MANIFEST.MF", "META-INF/jarjar/metadata.json", "pack.mcmeta", "manifest.json", "modrinth.index.json", "version.json",
}

// legacyFML are the names a class declaring a mod to FML before 1.13 carries, where a mod is
// declared by a class rather than in a metadata file: the @Mod and @API annotations and the
// DummyModContainer a coremod extends.
var legacyFML = [][]byte{
	[]byte("net/minecraftforge/fml/common/Mod;"), []byte("net/minecraftforge/fml/common/API;"), []byte("net/minecraftforge/fml/common/DummyModContainer"),
	[]byte("cpw/mods/fml/common/Mod;"), []byte("cpw/mods/fml/common/API;"), []byte("cpw/mods/fml/common/DummyModContainer"),
}

// keepJar is what a recording keeps of a downloaded zip: the files jarmeta reads, each jar nested
// under META-INF and each jar or zip a modpack ships kept the same way, and, in a jar with no metadata file, each class that declares
// a mod. A modpack, or a server pack with its mods folder, keeps every other file's name too,
// empty, so an import still lays out its overrides and reads which mods the server ships.
func keepJar(data []byte) (recordedJar, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return recordedJar{}, err
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	modern := names["fabric.mod.json"] || names["quilt.mod.json"] || names["META-INF/mods.toml"] || names["META-INF/neoforge.mods.toml"]
	pack := names["manifest.json"] || names["modrinth.index.json"]
	j := recordedJar{Files: map[string]string{}, Bytes: map[string][]byte{}, Jars: map[string]recordedJar{}}
	var rest []string
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		kept := slices.Contains(keptFiles, f.Name)
		nested := strings.HasPrefix(f.Name, "META-INF/") && strings.HasSuffix(f.Name, ".jar") || pack && (strings.HasSuffix(f.Name, ".jar") || strings.HasSuffix(f.Name, ".zip"))
		class := !modern && strings.HasSuffix(f.Name, ".class")
		if !kept && !nested && !class {
			rest = append(rest, f.Name)
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return recordedJar{}, err
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return recordedJar{}, err
		}
		switch {
		case nested:
			if inner, err := keepJar(content); err == nil {
				real := hashesOf(content)
				inner.Real = &real
				j.Jars[f.Name] = inner
			}
		case class:
			if slices.ContainsFunc(legacyFML, func(pkg []byte) bool { return bytes.Contains(content, pkg) }) {
				j.Bytes[f.Name] = content
			}
		case f.Name == "META-INF/MANIFEST.MF":
			j.Files[f.Name] = mainSection(string(content))
		case utf8.Valid(content):
			j.Files[f.Name] = string(content)
		default:
			j.Bytes[f.Name] = content
		}
	}
	if pack || slices.ContainsFunc(rest, func(name string) bool { return strings.Contains("/"+name, "/mods/") && strings.HasSuffix(name, ".jar") }) {
		for _, name := range rest {
			j.Files[name] = ""
		}
	}
	return j, nil
}

// mainSection is a jar manifest's main attributes, all jarmeta reads, without the per-entry
// digests a signed jar lists after them.
func mainSection(manifest string) string {
	for _, end := range []string{"\r\n\r\n", "\n\n"} {
		if i := strings.Index(manifest, end); i >= 0 {
			return manifest[:i+len(end)]
		}
	}
	return manifest
}

// keptHeaders are the response headers a recording keeps.
var keptHeaders = []string{"Content-Type", "Location"}

// liveClient reaches the real services, each redirect a response of its own, so every hop is
// recorded.
func liveClient() *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.TLSClientConfig = &tls.Config{}
	return &http.Client{Transport: t, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// fetchLive asks the real service for a request the recording lacks and adds the answer to it: a
// zip as a file, served as the real bytes, and anything else as an exchange, served with the
// recording's own files swapped in like the rest. A HEAD is fetched whole, so the file it asks
// about is recorded for the ranged reads that follow it.
func (r *replay) fetchLive(req *http.Request, u string, body []byte) (exchange, []byte, error) {
	method := req.Method
	if method == http.MethodHead {
		method = http.MethodGet
	}
	out, err := http.NewRequestWithContext(req.Context(), method, u, bytes.NewReader(body))
	if err != nil {
		return exchange{}, nil, err
	}
	out.Header = req.Header.Clone()
	out.Header.Del("Accept-Encoding")
	out.Header.Del("Range")
	resp, err := r.live.Do(out)
	if err != nil {
		return exchange{}, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return exchange{}, nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if resp.StatusCode == http.StatusOK && bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		if _, ok := r.liveFiles[u]; !ok {
			jar, err := keepJar(data)
			if err != nil {
				return exchange{}, nil, err
			}
			r.rec.Files = append(r.rec.Files, recordedFile{URL: u, fileHashes: hashesOf(data), Jar: jar})
			r.liveFiles[u] = data
		}
		return exchange{}, data, nil
	}
	ex := exchange{Method: req.Method, URL: u, RequestBody: string(body), Status: resp.StatusCode}
	for _, name := range keptHeaders {
		if value := resp.Header.Get(name); value != "" {
			if ex.Header == nil {
				ex.Header = map[string]string{}
			}
			ex.Header[name] = value
		}
	}
	switch {
	case req.Method == http.MethodHead:
	case utf8.Valid(data):
		ex.Body = string(data)
	default:
		ex.BodyBytes = data
	}
	key, err := requestKey(ex.Method, u, ex.RequestBody)
	if err != nil {
		return exchange{}, nil, err
	}
	if _, ok := r.responses[key]; !ok {
		r.rec.Exchanges = append(r.rec.Exchanges, ex)
	}
	served := ex
	served.Body = r.rw.rewrite(ex.Body)
	r.responses[key] = served
	return served, nil, nil
}

// writeRecording writes rec in a fixed order, so recording nothing new writes the same bytes, and
// refuses one that holds key anywhere.
func writeRecording(path string, rec *recording, key string) error {
	slices.SortFunc(rec.Exchanges, func(a, b exchange) int {
		return cmp.Or(cmp.Compare(a.URL, b.URL), cmp.Compare(a.Method, b.Method), cmp.Compare(a.RequestBody, b.RequestBody))
	})
	slices.SortFunc(rec.Files, func(a, b recordedFile) int { return cmp.Compare(a.URL, b.URL) })
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	if key != "" && bytes.Contains(data, []byte(key)) {
		return errors.New("the recording holds the CurseForge key")
	}
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return err
	}
	if _, err := zw.Write(data); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

func zipOf(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(data)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestKeepJarKeepsMetadataAndNestedJars(t *testing.T) {
	nested := zipOf(t, map[string][]byte{"fabric.mod.json": []byte(`{"id":"mixinextras"}`), "com/x/Big.class": []byte("code")})
	jar := zipOf(t, map[string][]byte{
		"fabric.mod.json":               []byte(`{"id":"sodium"}`),
		"META-INF/MANIFEST.MF":          []byte("Manifest-Version: 1.0\r\nFMLModType: LIBRARY\r\n\r\nName: a/B.class\r\nSHA-256-Digest: x\r\n"),
		"META-INF/jars/mixinextras.jar": nested,
		"assets/sodium/icon.png":        {0x89, 'P', 'N', 'G'},
		"net/caffeinemc/Sodium.class":   []byte("Lnet/minecraftforge/fml/common/Mod;"),
	})
	kept, err := keepJar(jar)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept.Files) != 2 || kept.Files["fabric.mod.json"] != `{"id":"sodium"}` || kept.Files["META-INF/MANIFEST.MF"] != "Manifest-Version: 1.0\r\nFMLModType: LIBRARY\r\n\r\n" {
		t.Fatalf("files: %v", kept.Files)
	}
	if len(kept.Bytes) != 0 {
		t.Fatalf("a modern jar kept classes: %v", kept.Bytes)
	}
	if inner := kept.Jars["META-INF/jars/mixinextras.jar"]; len(inner.Files) != 1 || inner.Files["fabric.mod.json"] != `{"id":"mixinextras"}` {
		t.Fatalf("nested: %+v", kept.Jars)
	}
}

func TestKeepJarKeepsALegacyForgeModsDeclaringClasses(t *testing.T) {
	declaring := append([]byte{0xca, 0xfe, 0xba, 0xbe}, "Lnet/minecraftforge/fml/common/Mod;"...)
	jar := zipOf(t, map[string][]byte{
		"mcmod.info":            []byte(`[{"modid":"rtg"}]`),
		"rtg/RTG.class":         declaring,
		"rtg/world/Biome.class": append([]byte{0xca, 0xfe, 0xba, 0xbe}, "Lnet/minecraftforge/fml/common/eventhandler/SubscribeEvent;"...),
	})
	kept, err := keepJar(jar)
	if err != nil {
		t.Fatal(err)
	}
	if kept.Files["mcmod.info"] == "" || !bytes.Equal(kept.Bytes["rtg/RTG.class"], declaring) || len(kept.Bytes) != 1 {
		t.Fatalf("kept %+v", kept)
	}
	rebuilt, err := kept.build()
	if err != nil {
		t.Fatal(err)
	}
	again, err := keepJar(rebuilt)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again.Bytes["rtg/RTG.class"], declaring) {
		t.Fatal("a rebuilt jar lost the class")
	}
}

func TestKeepJarKeepsAPacksFileNames(t *testing.T) {
	pack := zipOf(t, map[string][]byte{
		"manifest.json":            []byte(`{"name":"RLCraft"}`),
		"overrides/config/rtg.cfg": []byte("a long config"),
		"overrides/options.txt":    []byte("gamma:1"),
	})
	kept, err := keepJar(pack)
	if err != nil {
		t.Fatal(err)
	}
	if kept.Files["manifest.json"] != `{"name":"RLCraft"}` || kept.Files["overrides/config/rtg.cfg"] != "" || len(kept.Files) != 3 {
		t.Fatalf("kept %v", kept.Files)
	}
}

func TestAModpacksOwnJarKeepsItsRealHashes(t *testing.T) {
	mod := zipOf(t, map[string][]byte{"mcmod.info": []byte(`[{"modid":"antiquecities"}]`), "Big.class": []byte("code")})
	pack := zipOf(t, map[string][]byte{"manifest.json": []byte(`{"name":"RLCraft"}`), "overrides/mods/antiquecities.jar": mod})
	kept, err := keepJar(pack)
	if err != nil {
		t.Fatal(err)
	}
	inner := kept.Jars["overrides/mods/antiquecities.jar"]
	if inner.Real == nil || *inner.Real != hashesOf(mod) || inner.Files["mcmod.info"] == "" {
		t.Fatalf("kept %+v", kept)
	}
	rec := &recording{
		Files:     []recordedFile{{URL: "https://mediafilez.forgecdn.net/files/1/2/pack.zip", fileHashes: hashesOf(pack), Jar: kept}},
		Exchanges: []exchange{{Method: "POST", URL: "https://api.modrinth.com/v2/version_files", RequestBody: `{"hashes":["` + inner.Real.Sha1 + `"]}`, Status: 200, Body: "{}"}},
	}
	r, err := newReplay(rec)
	if err != nil {
		t.Fatal(err)
	}
	rebuiltInner, _ := inner.build()
	key, _ := requestKey("POST", "https://api.modrinth.com/v2/version_files", `{"hashes":["`+hashesOf(rebuiltInner).Sha1+`"]}`)
	if _, ok := r.responses[key]; !ok {
		t.Fatal("a lookup by the rebuilt jar's hash is not in the replay")
	}
}

func TestKeepJarKeepsAServerPacksModNamesOnly(t *testing.T) {
	mod := zipOf(t, map[string][]byte{"mcmod.info": []byte(`[{"modid":"rtg"}]`)})
	server := zipOf(t, map[string][]byte{"mods/RTG.jar": mod, "config/rtg.cfg": []byte("a long config"), "start.sh": []byte("java -jar")})
	kept, err := keepJar(server)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept.Jars) != 0 || len(kept.Files) != 3 || kept.Files["mods/RTG.jar"] != "" {
		t.Fatalf("kept %+v", kept)
	}
	resourcePack := zipOf(t, map[string][]byte{"pack.mcmeta": []byte(`{"pack":{}}`), "assets/x/textures/a.png": {0x89}})
	if kept, _ := keepJar(resourcePack); len(kept.Files) != 1 {
		t.Fatalf("a resource pack kept %v", kept.Files)
	}
}

// TestRecordingFillsOnlyMisses records through a replay against a fake upstream: what the
// recording holds is served from it, and only a miss reaches the service.
func TestRecordingFillsOnlyMisses(t *testing.T) {
	jar := zipOf(t, map[string][]byte{"fabric.mod.json": []byte(`{"id":"lithium"}`), "Big.class": []byte("code")})
	sum := sha512.Sum512(jar)
	var hits atomic.Int32
	var keys []string
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		keys = append(keys, r.Header.Get("x-api-key"))
		switch r.URL.Path {
		case "/v2/project/lithium":
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"id":"gvQqBUqZ","sha512":"`+hex.EncodeToString(sum[:])+`"}`)
		case "/lithium.jar":
			w.Write(jar)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	rec := &recording{Exchanges: []exchange{{Method: "GET", URL: "https://api.modrinth.com/v2/project/sodium", Status: 200, Body: `{"id":"AANobbMI"}`}}}
	r, err := newReplay(rec)
	if err != nil {
		t.Fatal(err)
	}
	r.live = fetchtest.Everything(upstream)
	front := httptest.NewTLSServer(r)
	defer front.Close()
	client := fetchtest.Everything(front)
	get := func(url string) []byte {
		t.Helper()
		req, _ := http.NewRequest("GET", url, nil)
		req.Header.Set("x-api-key", "secret-key")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return body
	}

	if body := get("https://api.modrinth.com/v2/project/sodium"); string(body) != `{"id":"AANobbMI"}` || hits.Load() != 0 {
		t.Fatalf("a recorded response went live: %s, %d hits", body, hits.Load())
	}
	get("https://api.modrinth.com/v2/project/lithium")
	head, err := client.Head("https://cdn.modrinth.com/lithium.jar")
	if err != nil {
		t.Fatal(err)
	}
	head.Body.Close()
	if head.ContentLength != int64(len(jar)) {
		t.Fatalf("a live HEAD gave size %d", head.ContentLength)
	}
	if got := get("https://cdn.modrinth.com/lithium.jar"); !bytes.Equal(got, jar) {
		t.Fatal("a live download was not the real file")
	}
	if hits.Load() != 2 || keys[0] != "secret-key" {
		t.Fatalf("%d hits, keys %v", hits.Load(), keys)
	}
	if misses := r.takeMisses(); len(misses) != 0 {
		t.Fatalf("misses: %v", misses)
	}

	path := filepath.Join(t.TempDir(), "responses.json.gz")
	if err := writeRecording(path, r.rec, "secret-key"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	again, err := readRecording(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Exchanges) != 2 || len(again.Files) != 1 {
		t.Fatalf("recorded %d exchanges and %d files", len(again.Exchanges), len(again.Files))
	}
	f := again.Files[0]
	if f.URL != "https://cdn.modrinth.com/lithium.jar" || f.Sha512 != hex.EncodeToString(sum[:]) || f.Size != int64(len(jar)) || len(f.Jar.Files) != 1 {
		t.Fatalf("file %+v", f)
	}
	if bytes.Contains(raw, []byte("secret-key")) {
		t.Fatal("the key was written")
	}
	if err := writeRecording(path, again, "secret-key"); err != nil {
		t.Fatal(err)
	}
	if rewritten, _ := os.ReadFile(path); !bytes.Equal(raw, rewritten) {
		t.Fatal("writing the same recording again changed it")
	}

	replayed, err := newReplay(again)
	if err != nil {
		t.Fatal(err)
	}
	served := httptest.NewTLSServer(replayed)
	defer served.Close()
	client = fetchtest.Everything(served)
	if body := get("https://api.modrinth.com/v2/project/lithium"); strings.Contains(string(body), hex.EncodeToString(sum[:])) {
		t.Fatalf("the replay kept the real hash: %s", body)
	}
}

func TestWritingARecordingRefusesTheKey(t *testing.T) {
	rec := &recording{Exchanges: []exchange{{Method: "GET", URL: "https://api.curseforge.com/v1/mods/1?key=secret-key", Status: 200}}}
	if err := writeRecording(filepath.Join(t.TempDir(), "r.json.gz"), rec, "secret-key"); err == nil {
		t.Fatal("a recording holding the key was written")
	}
}
