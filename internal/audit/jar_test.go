package audit

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
)

type zipFile struct {
	name string
	data []byte
}

func zipOf(t *testing.T, files ...zipFile) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		w, err := zw.Create(f.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(f.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func text(name, content string) zipFile { return zipFile{name, []byte(content)} }

func jarSubject(t *testing.T, data []byte) Subject {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mod.jar")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return Subject{Name: "mod.jar", Path: path}
}

func TestInspectJarReadsWhatEachLoaderDeclares(t *testing.T) {
	tests := []struct {
		name   string
		files  []zipFile
		id     string
		loader string
		mixins []string
	}{
		{"fabric", []zipFile{text("fabric.mod.json", `{"id":"fab","version":"1.0","entrypoints":{"main":"fab.Main"},"mixins":["fab.mixins.json"]}`)}, "fab", "fabric", []string{"fab.mixins.json"}},
		{"quilt", []zipFile{text("quilt.mod.json", `{"quilt_loader":{"id":"qui","version":"1.0","entrypoints":{"init":"qui.Init"}},"mixin":"qui.mixins.json"}`)}, "qui", "quilt", []string{"qui.mixins.json"}},
		{"neoforge", []zipFile{text("META-INF/neoforge.mods.toml", "[[mods]]\nmodId=\"neo\"\nversion=\"1.0\"\n[[mixins]]\nconfig=\"neo.mixins.json\"\n")}, "neo", "neoforge", []string{"neo.mixins.json"}},
		{"forge", []zipFile{text("META-INF/mods.toml", "[[mods]]\nmodId=\"frg\"\nversion=\"1.0\"\n"), text("META-INF/MANIFEST.MF", "Manifest-Version: 1.0\nMixinConfigs: frg.mixins.json\n")}, "frg", "forge", []string{"frg.mixins.json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rep, err := InspectJar(jarSubject(t, zipOf(t, tt.files...)))
			if err != nil {
				t.Fatal(err)
			}
			d := rep.Jar.Declares
			if d == nil || string(d.ID) != tt.id || d.Version != "1.0" || d.Loader != tt.loader {
				t.Fatalf("declares: %+v", d)
			}
			var mixins []string
			for _, m := range d.Mixins {
				mixins = append(mixins, string(m))
			}
			if !slices.Equal(mixins, tt.mixins) {
				t.Fatalf("mixins: %v", mixins)
			}
			if len(rep.Sha512) != 128 || rep.Size == 0 {
				t.Fatalf("hash and size: %+v", rep)
			}
		})
	}
}

func nestedJar(t *testing.T) []byte {
	t.Helper()
	inner := zipOf(t,
		text("fabric.mod.json", `{"id":"lib","version":"2.0"}`),
		text("natives/liblib.so", "elf"),
	)
	return zipOf(t,
		text("fabric.mod.json", `{"id":"outer","version":"1.0","jars":[{"file":"META-INF/jars/lib.jar"}]}`),
		zipFile{"META-INF/jars/lib.jar", inner},
		text("bin/helper.exe", "MZ"),
		text("outer/Main.class", "\xca\xfe\xba\xbe"),
	)
}

func TestInspectJarFollowsNestedJars(t *testing.T) {
	rep, err := InspectJar(jarSubject(t, nestedJar(t)))
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Jar.Files) != 4 || !slices.Equal(rep.Jar.Natives, []Untrusted{"bin/helper.exe"}) {
		t.Fatalf("outer files and natives: %+v", rep.Jar)
	}
	if len(rep.Jar.Nested) != 1 {
		t.Fatalf("nested: %+v", rep.Jar.Nested)
	}
	lib := rep.Jar.Nested[0]
	if lib.Path != "META-INF/jars/lib.jar" || lib.Declares == nil || lib.Declares.ID != "lib" || !slices.Equal(lib.Natives, []Untrusted{"natives/liblib.so"}) {
		t.Fatalf("nested jar: %+v", lib)
	}
}

func TestInspectJarListsATooDeepJarUnopened(t *testing.T) {
	data := zipOf(t, text("fabric.mod.json", `{"id":"deep"}`))
	for range nestedDepth + 2 {
		data = zipOf(t, zipFile{"in.jar", data})
	}
	rep, err := InspectJar(jarSubject(t, data))
	if err != nil {
		t.Fatal(err)
	}
	j, depth := rep.Jar, 0
	for len(j.Nested) == 1 && !j.Nested[0].Unopened {
		j, depth = j.Nested[0], depth+1
	}
	if depth != nestedDepth-1 || len(j.Nested) != 1 || !j.Nested[0].Unopened {
		t.Fatalf("stopped at depth %d: %+v", depth, j)
	}
}

func TestInspectJarRefusesAFileThatIsNoZip(t *testing.T) {
	_, err := InspectJar(jarSubject(t, []byte("not a zip")))
	var e *out.Error
	if !errors.As(err, &e) || e.Code != "jar-invalid" {
		t.Fatalf("got %v", err)
	}
}

func TestReadFileReadsFromANestedJar(t *testing.T) {
	s := jarSubject(t, nestedJar(t))
	rep, err := ReadFile(s, "META-INF/jars/lib.jar!/fabric.mod.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rep.Content), `"id":"lib"`) {
		t.Fatalf("content: %s", rep.Content)
	}
	for path, code := range map[string]string{
		"outer/Main.class":                 "class-file",
		"missing.json":                     "file-not-found",
		"META-INF/jars/lib.jar!/nope.json": "file-not-found",
		"bin/helper.exe!/x":                "jar-invalid",
	} {
		_, err := ReadFile(s, path)
		var e *out.Error
		if !errors.As(err, &e) || e.Code != code {
			t.Fatalf("%s: got %v, want %s", path, err, code)
		}
	}
}

func TestJarStringsAreMarkedUntrusted(t *testing.T) {
	rep, err := InspectJar(jarSubject(t, nestedJar(t)))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Jar struct {
			Declares struct {
				ID any `json:"id"`
			} `json:"declares"`
			Files []struct {
				Path any `json:"path"`
			} `json:"files"`
			Nested []struct {
				Path any `json:"path"`
			} `json:"nested"`
		} `json:"jar"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for _, v := range []any{raw.Jar.Declares.ID, raw.Jar.Files[0].Path, raw.Jar.Nested[0].Path} {
		m, ok := v.(map[string]any)
		if _, has := m["untrusted"]; !ok || !has || len(m) != 1 {
			t.Fatalf("a jar's string is %v in %s", v, data)
		}
	}
	file, err := ReadFile(jarSubject(t, nestedJar(t)), "fabric.mod.json")
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := json.Marshal(file); !strings.Contains(string(data), `"content":{"untrusted":"{`) {
		t.Fatalf("file content: %s", data)
	}
}

func TestUntrustedTextIsEscapedForATerminal(t *testing.T) {
	u := Untrusted("a\x1b[2Jb‮c\nd\te")
	if got := u.Line(); got != "a\\u001b[2Jb\\u202ec\\u000ad\\u0009e" {
		t.Fatalf("line: %q", got)
	}
	if got := u.Block(); got != "a\\u001b[2Jb\\u202ec\nd\te" {
		t.Fatalf("block: %q", got)
	}
	tick := Untrusted("run `rm -rf`\u202e")
	if got := tick.Line(); got != "run \\u0060rm -rf\\u0060\\u202e" {
		t.Fatalf("backticks on a line: %q", got)
	}
	if got := tick.Quoted(); got != "\"run \\u0060rm -rf\\u0060\\u202e\"" {
		t.Fatalf("quoted: %q", got)
	}
	if got := tick.Block(); got != "run `rm -rf`\\u202e" {
		t.Fatalf("a block is printed raw, so keeps its backticks: %q", got)
	}
}
