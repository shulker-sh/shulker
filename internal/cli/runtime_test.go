package cli

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrewmast/shulker/internal/meta"
)

const fakeJava = `#!/bin/sh
if [ "$1" = "-version" ]; then
  echo 'openjdk version "@VERSION@" 2025-10-21' >&2
  exit 0
fi
printf '%s\n' "$@" > args.txt
echo "[Server] Done"
while read -r line; do
  echo "[Server] got: $line"
  [ "$line" = "stop" ] && exit @EXIT@
done
exit 1
`

func (h *harness) fakeJDK(t *testing.T, version, exit string) string {
	t.Helper()
	jdk := filepath.Join(t.TempDir(), "jdk")
	if err := os.MkdirAll(filepath.Join(jdk, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := strings.NewReplacer("@VERSION@", version, "@EXIT@", exit).Replace(fakeJava)
	if err := os.WriteFile(filepath.Join(jdk, "bin", "java"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return jdk
}

const runtimeHome = "jre.bundle/Contents/Home"

type fakeRuntime struct {
	version string
	files   map[string]string
	links   map[string]string
	dirs    []string
	corrupt string
	hits    int
	missing bool
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{
		version: "25.0.1",
		files: map[string]string{
			runtimeHome + "/bin/java":    strings.NewReplacer("@VERSION@", "25.0.1", "@EXIT@", "0").Replace(fakeJava),
			runtimeHome + "/lib/modules": "modules",
		},
		links: map[string]string{runtimeHome + "/legal/LICENSE": "../lib/modules"},
		dirs:  []string{"jre.bundle", "jre.bundle/Contents", runtimeHome, runtimeHome + "/bin", runtimeHome + "/lib", runtimeHome + "/legal"},
	}
}

func sha1Hex(data []byte) string {
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])
}

func (r *fakeRuntime) manifest(base string) []byte {
	files := map[string]any{}
	for _, d := range r.dirs {
		files[d] = map[string]any{"type": "directory"}
	}
	for name, content := range r.files {
		sha := sha1Hex([]byte(content))
		files[name] = map[string]any{
			"type":       "file",
			"executable": strings.HasSuffix(name, "/bin/java"),
			"downloads":  map[string]any{"raw": map[string]any{"sha1": sha, "size": len(content), "url": base + "/jrt/objects/" + sha}},
		}
	}
	for name, target := range r.links {
		files[name] = map[string]any{"type": "link", "target": target}
	}
	data, _ := json.Marshal(map[string]any{"files": files})
	return data
}

func (r *fakeRuntime) register(mux *http.ServeMux, base func() string) {
	mux.HandleFunc("/jrt/all.json", func(w http.ResponseWriter, _ *http.Request) {
		platform, _ := meta.RuntimePlatform()
		if r.missing {
			writeJSON(w, map[string]any{platform: map[string]any{}})
			return
		}
		writeJSON(w, map[string]any{platform: map[string]any{"java-runtime-epsilon": []map[string]any{{
			"manifest": map[string]any{"sha1": sha1Hex(r.manifest(base())), "url": base() + "/jrt/epsilon.json"},
			"version":  map[string]any{"name": r.version},
		}}}})
	})
	mux.HandleFunc("/jrt/epsilon.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Write(r.manifest(base()))
	})
	mux.HandleFunc("/jrt/objects/", func(w http.ResponseWriter, req *http.Request) {
		r.hits++
		want := strings.TrimPrefix(req.URL.Path, "/jrt/objects/")
		for name, content := range r.files {
			if sha1Hex([]byte(content)) == want {
				if name == r.corrupt {
					content += "tampered"
				}
				w.Write([]byte(content))
				return
			}
		}
		http.NotFound(w, req)
	})
}

func (h *harness) managedJavaDir() string {
	return filepath.Join(h.cache, "java", "java-runtime-epsilon")
}

func (h *harness) readRuntimeMarker(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.managedJavaDir(), ".shulker-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}
