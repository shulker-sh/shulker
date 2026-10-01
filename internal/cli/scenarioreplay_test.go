package cli

import (
	"archive/zip"
	"bytes"
	"cmp"
	"compress/gzip"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"shulker.sh/shulker/internal/provider/curseforge"
)

// recording is a scenario's responses.json.gz: every response the steps got from the real
// services, and the files they downloaded, kept as the metadata a jar is rebuilt from.
type recording struct {
	// Recorded is when the recording was made, the time a replay runs at.
	Recorded  time.Time      `json:"recorded"`
	Exchanges []exchange     `json:"exchanges"`
	Files     []recordedFile `json:"files"`
}

// exchange is one request and the response it got. A request's body is kept for a POST, whose
// ids CurseForge and Modrinth take in the body rather than the URL. A response that isn't text is
// BodyBytes instead of Body.
type exchange struct {
	Method      string            `json:"method"`
	URL         string            `json:"url"`
	RequestBody string            `json:"requestBody,omitempty"`
	Status      int               `json:"status"`
	Header      map[string]string `json:"header,omitempty"`
	Body        string            `json:"body,omitempty"`
	BodyBytes   []byte            `json:"bodyBytes,omitempty"`
}

// recordedFile is a downloaded file: where it came from, the real file's hashes, and what is kept
// of its contents.
type recordedFile struct {
	URL string `json:"url"`
	fileHashes
	Jar recordedJar `json:"jar"`
}

// fileHashes are what a file is known by in the services' responses and the lookups the CLI
// sends: its sha1, sha512, size and CurseForge fingerprint.
type fileHashes struct {
	Sha1        string `json:"sha1"`
	Sha512      string `json:"sha512"`
	Size        int64  `json:"size"`
	Fingerprint uint32 `json:"fingerprint"`
}

func hashesOf(data []byte) fileHashes {
	s1, s512 := sha1.Sum(data), sha512.Sum512(data)
	return fileHashes{Sha1: hex.EncodeToString(s1[:]), Sha512: hex.EncodeToString(s512[:]), Size: int64(len(data)), Fingerprint: curseforge.Fingerprint(data)}
}

// recordedJar is what a jar keeps of itself: its metadata files by path, text in Files and
// anything else in Bytes, and its nested jars by path, each kept the same way with the real
// hashes it had, since the CLI looks a modpack's own jars up by them.
type recordedJar struct {
	Real  *fileHashes            `json:"real,omitempty"`
	Files map[string]string      `json:"files,omitempty"`
	Bytes map[string][]byte      `json:"bytes,omitempty"`
	Jars  map[string]recordedJar `json:"jars,omitempty"`
}

func readRecording(path string) (*recording, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	var rec recording
	if err := json.NewDecoder(zr).Decode(&rec); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &rec, nil
}

func (j recordedJar) build() ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range slices.Sorted(maps.Keys(j.Files)) {
		w, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := io.WriteString(w, j.Files[name]); err != nil {
			return nil, err
		}
	}
	for _, name := range slices.Sorted(maps.Keys(j.Bytes)) {
		w, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(j.Bytes[name]); err != nil {
			return nil, err
		}
	}
	for _, name := range slices.Sorted(maps.Keys(j.Jars)) {
		nested, err := j.Jars[name].build()
		if err != nil {
			return nil, err
		}
		w, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(nested); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// rebuilt is a recorded file built again from its metadata, beside the real values it stands in for.
type rebuilt struct {
	real        fileHashes
	data        []byte
	sha1        string
	sha512      string
	fingerprint uint32
}

func rebuild(real fileHashes, j recordedJar) (*rebuilt, error) {
	data, err := j.build()
	if err != nil {
		return nil, err
	}
	h := hashesOf(data)
	return &rebuilt{real: real, data: data, sha1: h.Sha1, sha512: h.Sha512, fingerprint: h.Fingerprint}, nil
}

// nestedRebuilt are the jars nested in j that kept their real hashes, at any depth.
func nestedRebuilt(j recordedJar) ([]*rebuilt, error) {
	var all []*rebuilt
	for _, name := range slices.Sorted(maps.Keys(j.Jars)) {
		inner := j.Jars[name]
		if inner.Real != nil {
			b, err := rebuild(*inner.Real, inner)
			if err != nil {
				return nil, err
			}
			all = append(all, b)
		}
		deeper, err := nestedRebuilt(inner)
		if err != nil {
			return nil, err
		}
		all = append(all, deeper...)
	}
	return all, nil
}

// replay serves a recording: each file rebuilt, and every body with the real files' hashes and
// sizes swapped for the rebuilt ones'. A request it lacks gets a 404 and is kept as a miss, unless
// live is set: then it is fetched from the real service and added to rec.
type replay struct {
	rec       *recording
	rw        *rewriter
	live      *http.Client
	mu        sync.Mutex
	responses map[string]exchange
	files     map[string]*rebuilt
	// liveFiles are the files fetched live this run, by URL, served again as they came.
	liveFiles map[string][]byte
	misses    []string
}

func newReplay(rec *recording) (*replay, error) {
	r := &replay{rec: rec, responses: map[string]exchange{}, files: map[string]*rebuilt{}, liveFiles: map[string][]byte{}}
	var all []*rebuilt
	for _, f := range rec.Files {
		b, err := rebuild(f.fileHashes, f.Jar)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.URL, err)
		}
		nested, err := nestedRebuilt(f.Jar)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.URL, err)
		}
		r.files[f.URL] = b
		all = append(all, b)
		all = append(all, nested...)
	}
	rw := newRewriter(all)
	r.rw = rw
	for _, ex := range rec.Exchanges {
		ex.Body = rw.rewrite(ex.Body)
		key, err := requestKey(ex.Method, ex.URL, rw.rewrite(ex.RequestBody))
		if err != nil {
			return nil, err
		}
		if _, ok := r.responses[key]; !ok {
			r.responses[key] = ex
		}
	}
	return r, nil
}

func (r *replay) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	u := "https://" + req.Host + req.URL.RequestURI()
	if req.Method == http.MethodGet || req.Method == http.MethodHead {
		if data, ok := r.file(u); ok {
			http.ServeContent(w, req, "", time.Time{}, bytes.NewReader(data))
			return
		}
	}
	body, _ := io.ReadAll(req.Body)
	key, err := requestKey(req.Method, u, string(body))
	r.mu.Lock()
	ex, ok := r.responses[key]
	r.mu.Unlock()
	if err == nil && !ok && r.live != nil {
		var file []byte
		ex, file, err = r.fetchLive(req, u, body)
		if err == nil && file != nil {
			http.ServeContent(w, req, "", time.Time{}, bytes.NewReader(file))
			return
		}
		ok = err == nil
	}
	if err != nil || !ok {
		miss := req.Method + " " + req.Host + req.URL.RequestURI()
		if err != nil {
			miss += " (" + err.Error() + ")"
		}
		r.mu.Lock()
		r.misses = append(r.misses, miss)
		r.mu.Unlock()
		http.Error(w, "not in the recording", http.StatusNotFound)
		return
	}
	for name, value := range ex.Header {
		w.Header().Set(name, value)
	}
	w.WriteHeader(ex.Status)
	if ex.BodyBytes != nil {
		_, _ = w.Write(ex.BodyBytes)
		return
	}
	_, _ = io.WriteString(w, ex.Body)
}

// file is the file served at u: rebuilt from the recording, or as it came when fetched live this run.
func (r *replay) file(u string) ([]byte, bool) {
	if f, ok := r.files[u]; ok {
		return f.data, true
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	data, ok := r.liveFiles[u]
	return data, ok
}

// takeMisses returns the requests the recording lacked since the last call.
func (r *replay) takeMisses() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	misses := r.misses
	r.misses = nil
	return misses
}

// requestKey is what a request is looked up by: its method, host, path, query in a fixed order,
// and its body, with a JSON body's spacing and key order normalised.
func requestKey(method, rawURL, body string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	var v any
	if json.Unmarshal([]byte(body), &v) == nil {
		sortLookups(v)
		normal, _ := json.Marshal(v)
		body = string(normal)
	}
	return method + " " + u.Host + u.EscapedPath() + "?" + u.Query().Encode() + "\n" + body, nil
}

// sortLookups puts each list of ids or hashes at the top of a request body in order. A lookup's
// list is a set, and one of hashes or fingerprints comes in another order once the files are
// rebuilt, since CurseForge's Filed and Modrinth's hash lookup sort it.
func sortLookups(v any) {
	body, ok := v.(map[string]any)
	if !ok {
		return
	}
	for _, value := range body {
		list, ok := value.([]any)
		if !ok {
			continue
		}
		slices.SortFunc(list, func(a, b any) int {
			x, xNumber := a.(float64)
			y, yNumber := b.(float64)
			if xNumber && yNumber {
				return cmp.Compare(x, y)
			}
			return cmp.Compare(fmt.Sprint(a), fmt.Sprint(b))
		})
	}
}

// sizeKeys and fingerprintKeys are the keys whose numbers are a file's size or fingerprint, in
// any service's responses, and in the fingerprint lookup CurseForge takes.
var (
	sizeKeys        = []string{"size", "fileLength", "fileSizeOnDisk"}
	fingerprintKeys = []string{"fileFingerprint", "fingerprints"}
)

// rewriter swaps the real files' values for the rebuilt ones' in a body: a hash string wherever it
// appears, and a number only under a size or fingerprint key. Two files can share a size, so a
// number goes to the file whose hash sits in the same object, if any does.
type rewriter struct {
	hashes *strings.Replacer
	files  []*rebuilt
	byHash map[string]*rebuilt
}

func newRewriter(files []*rebuilt) *rewriter {
	// A Replacer tries its pairs in order, so every sha512 goes before any sha1 that could be its prefix.
	var long, short []string
	byHash := map[string]*rebuilt{}
	for _, f := range files {
		long = append(long, f.real.Sha512, f.sha512)
		short = append(short, f.real.Sha1, f.sha1)
		byHash[f.sha1], byHash[f.sha512] = f, f
	}
	return &rewriter{hashes: strings.NewReplacer(append(long, short...)...), files: files, byHash: byHash}
}

func (rw *rewriter) rewrite(body string) string {
	if body == "" || len(rw.files) == 0 {
		return body
	}
	body = rw.hashes.Replace(body)
	dec := json.NewDecoder(strings.NewReader(body))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil {
		return body
	}
	v, _ = rw.numbers(v)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if enc.Encode(v) != nil {
		return body
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

// numbers rewrites the size and fingerprint numbers in v, returning it with the rebuilt files
// whose hashes appear anywhere inside it.
func (rw *rewriter) numbers(v any) (any, []*rebuilt) {
	switch v := v.(type) {
	case string:
		if f, ok := rw.byHash[v]; ok {
			return v, []*rebuilt{f}
		}
	case []any:
		var found []*rebuilt
		for i, child := range v {
			var in []*rebuilt
			v[i], in = rw.numbers(child)
			found = append(found, in...)
		}
		return v, found
	case map[string]any:
		var found []*rebuilt
		for key, child := range v {
			var in []*rebuilt
			v[key], in = rw.numbers(child)
			found = append(found, in...)
		}
		for key, child := range v {
			switch {
			case slices.Contains(sizeKeys, key):
				v[key] = rw.number(child, found, func(f *rebuilt) (int64, int64) { return f.real.Size, int64(len(f.data)) })
			case slices.Contains(fingerprintKeys, key):
				v[key] = rw.number(child, found, func(f *rebuilt) (int64, int64) { return int64(f.real.Fingerprint), int64(f.fingerprint) })
			}
		}
		return v, found
	}
	return v, nil
}

// number swaps a real value for its rebuilt one, through an array of them too: the value of a
// file in scope if one has it, else of every file that has it, when they all agree.
func (rw *rewriter) number(v any, scope []*rebuilt, values func(*rebuilt) (real, rebuilt int64)) any {
	if list, ok := v.([]any); ok {
		for i, item := range list {
			list[i] = rw.number(item, scope, values)
		}
		return list
	}
	n, ok := v.(json.Number)
	if !ok {
		return v
	}
	real, err := n.Int64()
	if err != nil {
		return v
	}
	pick := func(files []*rebuilt) (int64, bool) {
		var out []int64
		for _, f := range files {
			if r, b := values(f); r == real {
				out = append(out, b)
			}
		}
		slices.Sort(out)
		out = slices.Compact(out)
		if len(out) != 1 {
			return 0, false
		}
		return out[0], true
	}
	if b, ok := pick(scope); ok {
		return json.Number(strconv.FormatInt(b, 10))
	}
	if b, ok := pick(rw.files); ok {
		return json.Number(strconv.FormatInt(b, 10))
	}
	return v
}

func TestRewriterSwapsHashesAndOnlyFileNumbers(t *testing.T) {
	a := &rebuilt{real: fileHashes{Sha1: strings.Repeat("a", 40), Sha512: strings.Repeat("a", 128), Size: 500, Fingerprint: 7}, data: make([]byte, 10), sha1: strings.Repeat("1", 40), sha512: strings.Repeat("1", 128), fingerprint: 70}
	b := &rebuilt{real: fileHashes{Sha1: strings.Repeat("b", 40), Sha512: strings.Repeat("b", 128), Size: 500, Fingerprint: 8}, data: make([]byte, 20), sha1: strings.Repeat("2", 40), sha512: strings.Repeat("2", 128), fingerprint: 80}
	rw := newRewriter([]*rebuilt{a, b})
	body := fmt.Sprintf(`{"downloads":500,"files":[{"hashes":[{"value":%q}],"fileLength":500,"fileFingerprint":7},{"sha1":%q,"size":500}],"note":"sha %s"}`, a.real.Sha1, b.real.Sha1, b.real.Sha512)
	want := fmt.Sprintf(`{"downloads":500,"files":[{"fileFingerprint":70,"fileLength":10,"hashes":[{"value":%q}]},{"sha1":%q,"size":20}],"note":"sha %s"}`, a.sha1, b.sha1, b.sha512)
	if got := rw.rewrite(body); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if got := rw.rewrite(`{"fingerprints":[8,7,9]}`); got != `{"fingerprints":[80,70,9]}` {
		t.Fatalf("fingerprint lookup: %s", got)
	}
	if got := rw.rewrite(`{"size":500}`); got != `{"size":500}` {
		t.Fatalf("a size two files share was guessed: %s", got)
	}
	if got := rw.rewrite("<sha1>" + a.real.Sha1 + "</sha1>"); got != "<sha1>"+a.sha1+"</sha1>" {
		t.Fatalf("a hash outside JSON: %s", got)
	}
}

func TestLookupListsMatchInAnyOrder(t *testing.T) {
	recorded, _ := requestKey("POST", "https://api.curseforge.com/v1/fingerprints", `{"fingerprints":[30,10,20]}`)
	sent, _ := requestKey("POST", "https://api.curseforge.com/v1/fingerprints", `{"fingerprints": [10, 20, 30]}`)
	if recorded != sent {
		t.Fatalf("%q != %q", recorded, sent)
	}
	recorded, _ = requestKey("POST", "https://api.modrinth.com/v2/version_files", `{"algorithm":"sha1","hashes":["bb","aa"]}`)
	sent, _ = requestKey("POST", "https://api.modrinth.com/v2/version_files", `{"hashes":["aa","bb"],"algorithm":"sha1"}`)
	if recorded != sent {
		t.Fatalf("%q != %q", recorded, sent)
	}
}
