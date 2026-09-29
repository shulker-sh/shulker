package audit

import (
	"archive/zip"
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/jarmeta"
	"shulker.sh/shulker/internal/loader"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

// Untrusted is text read from inside a jar, written by whoever made the jar, who may have written
// it to instruct an agent reading it. It marshals as {"untrusted": "…"} so a reader of the JSON can
// tell what a jar says from what shulker says.
type Untrusted string

type untrustedJSON struct {
	Untrusted string `json:"untrusted"`
}

func (u Untrusted) MarshalJSON() ([]byte, error) { return json.Marshal(untrustedJSON{string(u)}) }

func (u *Untrusted) UnmarshalJSON(data []byte) error {
	var v untrustedJSON
	err := json.Unmarshal(data, &v)
	*u = Untrusted(v.Untrusted)
	return err
}

// Line is u for a terminal on one line, every control and format character escaped, so a jar can't
// move the cursor, recolour the screen or reorder text with bidi overrides. Backticks are escaped
// too, since output markup styles text between them as a command to run.
func (u Untrusted) Line() string { return escape(string(u), false) }

// Quoted is u as a Go string literal for one line of a terminal, escaped as Line escapes it.
func (u Untrusted) Quoted() string {
	return strings.ReplaceAll(strconv.Quote(string(u)), "`", `\u0060`)
}

// Block is u for a terminal with its newlines and tabs kept and every other control and format
// character escaped.
func (u Untrusted) Block() string { return escape(string(u), true) }

func escape(s string, block bool) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case block && (r == '\n' || r == '\t'):
			b.WriteRune(r)
		case r == utf8.RuneError || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || !block && r == '`':
			fmt.Fprintf(&b, "\\u%04x", r)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// nestedSeparator joins the path of a nested jar to a path inside it, as in Java's jar URLs.
const nestedSeparator = "!/"

// nestedDepth bounds how deep nested jars are followed, since a zip can hold itself.
const nestedDepth = 16

// maxNested is the most a nested jar may inflate to before it is listed without being opened.
const maxNested = 512 << 20

// Subject is the jar an inspection command reads: a lock entry's cached file, or a jar on disk.
type Subject struct {
	Name string
	Path string
	// Origin is the lock entry the jar is, nil for a file on disk the lock doesn't hold.
	Origin *Origin
	Loader loader.Loader
}

// Origin is where a locked jar comes from, as its lock entry records it.
type Origin struct {
	Key           string `json:"key"`
	Provider      string `json:"provider,omitempty"`
	Project       string `json:"project,omitempty"`
	Version       string `json:"version,omitempty"`
	VersionNumber string `json:"versionNumber,omitempty"`
	// Host is where the jar downloads from.
	Host string `json:"host,omitempty"`
	// File is the local file an unpublished entry reads.
	File    string `json:"file,omitempty"`
	Modpack string `json:"modpack,omitempty"`
	// OffProvider marks an entry that names a provider but downloads from outside its hosts.
	OffProvider bool `json:"offProvider"`
}

// IsPath reports whether arg names a file on disk rather than a lock entry.
func IsPath(arg string) bool {
	lower := strings.ToLower(arg)
	return strings.ContainsAny(arg, `/\`) || strings.HasSuffix(lower, ".jar") || strings.HasSuffix(lower, ".zip")
}

// Resolve finds the jar arg names: a file on disk when it reads as a path, else the lock entry
// with that key. b may be nil outside a project, when only a path resolves; a path whose hash the
// lock holds takes that entry's origin.
func Resolve(b *build.Builder, arg string) (Subject, error) {
	if IsPath(arg) {
		return resolvePath(b, arg)
	}
	if b == nil {
		e := out.Errorf("file-not-found", "%s is not a jar on disk", arg)
		e.Help = "name a lock entry from inside a project, or a path to a jar"
		return Subject{}, e
	}
	origins := entryOrigins(b)
	o, ok := origins[arg]
	if !ok {
		e := out.Errorf("mod-not-found", "%s is not in the lock", arg)
		e.Candidates, e.Given = slices.Sorted(maps.Keys(origins)), arg
		return Subject{}, e
	}
	sha := entrySha512(b, arg)
	if sha == "" || !b.Cache.Has(sha) {
		e := out.Errorf("not-installed", "%s is not in the cache", arg)
		e.Help = "run `shulker install`"
		return Subject{}, e
	}
	return Subject{Name: arg, Path: b.Cache.Object(sha), Origin: o, Loader: loader.Running(b.Lock)}, nil
}

func resolvePath(b *build.Builder, arg string) (Subject, error) {
	info, err := os.Stat(arg)
	if err != nil || !info.Mode().IsRegular() {
		return Subject{}, out.Errorf("file-not-found", "%s is not a file", arg)
	}
	s := Subject{Name: filepath.Base(arg), Path: arg}
	if b == nil {
		return s, nil
	}
	s.Loader = loader.Running(b.Lock)
	sum, err := hashFile(sha512.New(), arg)
	if err != nil {
		return Subject{}, err
	}
	origins := entryOrigins(b)
	for _, key := range slices.Sorted(maps.Keys(origins)) {
		if entrySha512(b, key) == sum {
			s.Origin = origins[key]
			break
		}
	}
	return s, nil
}

// entryOrigins are the lock's mods and packs by key, each with where it comes from.
func entryOrigins(b *build.Builder) map[string]*Origin {
	off := map[string]bool{}
	for _, m := range build.Mismatches(b.Providers, b.Lock) {
		off[m.Key] = true
	}
	origins := map[string]*Origin{}
	add := func(key, provider, project, version, number, file, modpack string, u *string) {
		o := &Origin{Key: key, Provider: provider, Project: project, Version: version, VersionNumber: number, File: file, Modpack: modpack, OffProvider: off[key]}
		if u != nil && file == "" {
			o.Host = build.HostOf(*u)
		}
		origins[key] = o
	}
	for key, m := range b.Lock.Mods {
		add(key, m.Provider, m.Project, m.Version, m.VersionNumber, m.File, m.Modpack, m.URL)
	}
	for _, kind := range manifest.PackKinds {
		for key, p := range b.Lock.Packs(kind) {
			add(key, p.Provider, p.Project, p.Version, p.VersionNumber, p.File, p.Modpack, p.URL)
		}
	}
	return origins
}

func entrySha512(b *build.Builder, key string) string {
	if m, ok := b.Lock.Mods[key]; ok {
		return m.Sha512
	}
	for _, kind := range manifest.PackKinds {
		if p, ok := b.Lock.Packs(kind)[key]; ok {
			return p.Sha512
		}
	}
	return ""
}

// JarReport is what audit jar shows of a jar.
type JarReport struct {
	Name   string  `json:"name"`
	Sha512 string  `json:"sha512"`
	Size   int64   `json:"size"`
	Origin *Origin `json:"origin"`
	Jar    Jar     `json:"jar"`
}

// Jar is one jar's contents: what it declares, its files, and the jars nested in it.
type Jar struct {
	// Path is where a nested jar sits in its parent, empty for the outermost.
	Path     Untrusted `json:"path,omitempty"`
	Declares *Declared `json:"declares"`
	// MetadataError says why metadata the jar holds couldn't be read.
	MetadataError Untrusted   `json:"metadataError,omitempty"`
	Files         []JarFile   `json:"files"`
	Natives       []Untrusted `json:"natives"`
	Nested        []Jar       `json:"nested"`
	// Unopened marks a nested jar too large or too deep to open.
	Unopened bool `json:"unopened,omitempty"`
}

// Declared is the mod a jar's metadata declares.
type Declared struct {
	ID          Untrusted    `json:"id"`
	Version     Untrusted    `json:"version"`
	Loader      string       `json:"loader"`
	Entrypoints []Entrypoint `json:"entrypoints"`
	Mixins      []Untrusted  `json:"mixins"`
}

// Entrypoint is a class or member the jar asks its loader to call, under the kind of entrypoint.
type Entrypoint struct {
	Kind  Untrusted `json:"kind"`
	Value Untrusted `json:"value"`
}

// JarFile is a file inside a jar, with its inflated size.
type JarFile struct {
	Path Untrusted `json:"path"`
	Size int64     `json:"size"`
}

// nativeSuffixes are the native libraries and executables a jar can carry.
var nativeSuffixes = []string{".dll", ".so", ".dylib", ".jnilib", ".exe"}

// IsNative reports whether a file in a jar is a native library or an executable.
func IsNative(name string) bool {
	lower := strings.ToLower(name)
	for _, suffix := range nativeSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return strings.Contains(lower, ".so.")
}

// IsNestedJar reports whether a file in a jar is a jar itself: Fabric and Quilt nest them under
// META-INF/jars/ and Forge and NeoForge under META-INF/jarjar/, and any other is followed too.
func IsNestedJar(name string) bool { return strings.HasSuffix(strings.ToLower(name), ".jar") }

// InspectJar reads s's jar: its hash, its origin, and its contents with every nested jar's.
func InspectJar(s Subject) (*JarReport, error) {
	data, zr, err := readJar(s)
	if err != nil {
		return nil, err
	}
	sum := sha512.Sum512(data)
	rep := &JarReport{Name: s.Name, Sha512: hex.EncodeToString(sum[:]), Size: int64(len(data)), Origin: s.Origin}
	rep.Jar = inspect(zr, s.Loader, 0)
	return rep, nil
}

func readJar(s Subject) ([]byte, *zip.Reader, error) {
	data, err := os.ReadFile(s.Path)
	if err != nil {
		return nil, nil, out.Errorf("jar-invalid", "shulker can't read %s", s.Name).WithCause("read", err)
	}
	zr, err := openJar(s.Name, data)
	return data, zr, err
}

func openJar(name string, data []byte) (*zip.Reader, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, out.Errorf("jar-invalid", "%s isn't a readable jar", name).WithCause("zip", err)
	}
	return zr, nil
}

func inspect(zr *zip.Reader, l loader.Loader, depth int) Jar {
	j := Jar{Files: []JarFile{}, Natives: []Untrusted{}, Nested: []Jar{}}
	info, err := jarmeta.ReadZip(zr, l)
	if err != nil {
		j.MetadataError = Untrusted(err.Error())
	}
	if info != nil {
		j.Declares = declared(info)
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		j.Files = append(j.Files, JarFile{Path: Untrusted(f.Name), Size: int64(f.UncompressedSize64)})
		if IsNative(f.Name) {
			j.Natives = append(j.Natives, Untrusted(f.Name))
		}
		if !IsNestedJar(f.Name) {
			continue
		}
		inner, ok := openNested(f, depth)
		if !ok {
			j.Nested = append(j.Nested, Jar{Path: Untrusted(f.Name), Files: []JarFile{}, Natives: []Untrusted{}, Nested: []Jar{}, Unopened: true})
			continue
		}
		child := inspect(inner, l, depth+1)
		child.Path = Untrusted(f.Name)
		j.Nested = append(j.Nested, child)
	}
	return j
}

// openNested opens a jar inside another, refusing one nested too deep or inflating past maxNested.
func openNested(f *zip.File, depth int) (*zip.Reader, bool) {
	if depth+1 >= nestedDepth || f.UncompressedSize64 > maxNested {
		return nil, false
	}
	data, err := readEntry(f)
	if err != nil {
		return nil, false
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	return zr, err == nil
}

// readEntry reads one file from a jar, refusing one that inflates past maxNested whatever its
// header claims.
func readEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxNested+1))
	if err == nil && len(data) > maxNested {
		err = out.Errorf("jar-invalid", "%s inflates past %d MiB", f.Name, maxNested>>20)
	}
	return data, err
}

func declared(info *jarmeta.Info) *Declared {
	d := &Declared{ID: Untrusted(info.ID), Version: Untrusted(info.Version), Loader: info.Loader, Entrypoints: []Entrypoint{}, Mixins: []Untrusted{}}
	for _, e := range info.Entrypoints {
		d.Entrypoints = append(d.Entrypoints, Entrypoint{Kind: Untrusted(e.Kind), Value: Untrusted(e.Value)})
	}
	for _, m := range info.Mixins {
		d.Mixins = append(d.Mixins, Untrusted(m))
	}
	return d
}

// FileReport is one file read from inside a jar.
type FileReport struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	Content Untrusted `json:"content"`
}

// ReadFile reads the file at path inside s's jar. A path into a nested jar joins each jar's path
// with "!/", as in META-INF/jars/lib.jar!/fabric.mod.json. A class file is refused, since its bytes
// aren't text; audit class reads it.
func ReadFile(s Subject, path string) (*FileReport, error) {
	if strings.HasSuffix(path, ".class") {
		e := out.Errorf("class-file", "%s is a class file", path)
		class := path
		if i := strings.LastIndex(path, nestedSeparator); i >= 0 {
			class = path[i+len(nestedSeparator):]
		}
		e.Help = "run `shulker audit class " + s.Name + " " + strings.TrimSuffix(class, ".class") + "` to read it"
		return nil, e
	}
	_, zr, err := readJar(s)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(path, nestedSeparator)
	var data []byte
	for i, part := range parts {
		if i > 0 {
			if zr, err = openJar(strings.Join(parts[:i], nestedSeparator), data); err != nil {
				return nil, err
			}
		}
		f := findFile(zr, part)
		if f == nil {
			return nil, out.Errorf("file-not-found", "%s holds no file %s", s.Name, strings.Join(parts[:i+1], nestedSeparator))
		}
		if data, err = readEntry(f); err != nil {
			return nil, err
		}
	}
	return &FileReport{Name: s.Name, Path: path, Size: int64(len(data)), Content: Untrusted(data)}, nil
}

func findFile(zr *zip.Reader, name string) *zip.File {
	name = strings.TrimPrefix(name, "/")
	for _, f := range zr.File {
		if f.Name == name && !f.FileInfo().IsDir() {
			return f
		}
	}
	return nil
}
