package envtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"shulker.sh/shulker/internal/mojang"
)

// RuntimeHome is where the fake runtime's java sits under its component directory.
const RuntimeHome = "jre.bundle/Contents/Home"

const fakeJava = `#!/bin/sh
if [ "$1" = "-version" ]; then
  echo 'openjdk version "25.0.1" 2025-10-21' >&2
  exit 0
fi
exit 0
`

// Piston is a fake of Mojang's index and download hosts for one version, 26.2, with one library,
// one asset index naming two assets, a client and a server jar, and a Java runtime index with one
// component, java-runtime-epsilon. Hits counts every download served. Profiles is the profile
// service's players, name to uuid, empty until a test adds some; ProfileHits counts its lookups.
type Piston struct {
	srv         *httptest.Server
	Hits        atomic.Int64
	Client      []byte
	Server      []byte
	Lib         []byte
	Assets      map[string]string
	Profiles    map[string]string
	ProfileHits atomic.Int64
}

// NewPiston starts the fake on a test server.
func NewPiston(t *testing.T) *Piston {
	t.Helper()
	p := &Piston{
		Client:   []byte("client jar"),
		Server:   []byte("server jar"),
		Lib:      []byte("brigadier"),
		Assets:   map[string]string{"icons/icon_16x16.png": "icon", "sounds/click.ogg": "click"},
		Profiles: map[string]string{},
	}
	objects := map[string]any{}
	for name, body := range p.Assets {
		objects[name] = map[string]any{"hash": Sha1Hex([]byte(body)), "size": len(body)}
	}
	index, _ := json.Marshal(map[string]any{"objects": objects})
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"latest":   map[string]string{"release": "26.2"},
			"versions": []map[string]string{{"id": "26.2", "type": "release", "url": base + "/26.2.json"}},
		})
	})
	mux.HandleFunc("/26.2.json", func(w http.ResponseWriter, r *http.Request) {
		p.Hits.Add(1)
		json.NewEncoder(w).Encode(map[string]any{
			"id":        "26.2",
			"type":      "release",
			"mainClass": "net.minecraft.client.main.Main",
			"arguments": map[string]any{
				"game": []any{"--username", "${auth_player_name}", "--uuid", "${auth_uuid}", "--accessToken", "${auth_access_token}", "--gameDir", "${game_directory}",
					map[string]any{"rules": []any{map[string]any{"action": "allow", "features": map[string]bool{"has_custom_resolution": true}}}, "value": []string{"--width", "${resolution_width}", "--height", "${resolution_height}"}}},
				"jvm": []string{"-Djava.library.path=${natives_directory}", "-cp", "${classpath}"},
			},
			"libraries":   []map[string]any{{"name": "com.mojang:brigadier:1.3.10", "downloads": map[string]any{"artifact": map[string]any{"path": "com/mojang/brigadier/1.3.10/brigadier-1.3.10.jar", "url": base + "/brigadier.jar", "sha1": Sha1Hex(p.Lib), "size": len(p.Lib)}}}},
			"javaVersion": map[string]any{"component": "java-runtime-epsilon", "majorVersion": 25},
			"assetIndex":  map[string]any{"id": "26", "url": base + "/assets/26.json", "sha1": Sha1Hex(index), "size": len(index)},
			"downloads": map[string]any{
				"client": map[string]any{"url": base + "/client.jar", "sha1": Sha1Hex(p.Client), "size": len(p.Client)},
				"server": map[string]any{"url": base + "/server.jar", "sha1": Sha1Hex(p.Server), "size": len(p.Server)},
			},
		})
	})
	serve := func(path string, body []byte) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			p.Hits.Add(1)
			w.Write(body)
		})
	}
	serve("/client.jar", p.Client)
	serve("/server.jar", p.Server)
	serve("/brigadier.jar", p.Lib)
	serve("/assets/26.json", index)
	mux.HandleFunc("/resources/", func(w http.ResponseWriter, r *http.Request) {
		p.Hits.Add(1)
		for _, body := range p.Assets {
			if strings.HasSuffix(r.URL.Path, Sha1Hex([]byte(body))) {
				w.Write([]byte(body))
				return
			}
		}
		http.NotFound(w, r)
	})
	files := map[string]string{RuntimeHome + "/bin/java": fakeJava, RuntimeHome + "/lib/modules": "modules"}
	dirs := []string{"jre.bundle", "jre.bundle/Contents", RuntimeHome, RuntimeHome + "/bin", RuntimeHome + "/lib"}
	runtime := func() []byte {
		entries := map[string]any{}
		for _, d := range dirs {
			entries[d] = map[string]any{"type": "directory"}
		}
		for name, content := range files {
			sha := Sha1Hex([]byte(content))
			entries[name] = map[string]any{
				"type":       "file",
				"executable": strings.HasSuffix(name, "/bin/java"),
				"downloads":  map[string]any{"raw": map[string]any{"sha1": sha, "size": len(content), "url": base + "/jrt/objects/" + sha}},
			}
		}
		data, _ := json.Marshal(map[string]any{"files": entries})
		return data
	}
	mux.HandleFunc("/jrt/all.json", func(w http.ResponseWriter, _ *http.Request) {
		platform, _ := mojang.RuntimePlatform()
		json.NewEncoder(w).Encode(map[string]any{platform: map[string]any{"java-runtime-epsilon": []map[string]any{{
			"manifest": map[string]any{"sha1": Sha1Hex(runtime()), "url": base + "/jrt/epsilon.json"},
			"version":  map[string]any{"name": "25.0.1"},
		}}}})
	})
	mux.HandleFunc("/jrt/epsilon.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Write(runtime())
	})
	mux.HandleFunc("/jrt/objects/", func(w http.ResponseWriter, r *http.Request) {
		p.Hits.Add(1)
		want := strings.TrimPrefix(r.URL.Path, "/jrt/objects/")
		for _, content := range files {
			if Sha1Hex([]byte(content)) == want {
				w.Write([]byte(content))
				return
			}
		}
		http.NotFound(w, r)
	})
	mux.HandleFunc("/mojang/profiles/minecraft", func(w http.ResponseWriter, r *http.Request) {
		p.ProfileHits.Add(1)
		var names []string
		_ = json.NewDecoder(r.Body).Decode(&names)
		profiles := []map[string]string{}
		for _, n := range names {
			for name, id := range p.Profiles {
				if strings.EqualFold(name, n) {
					profiles = append(profiles, map[string]string{"id": strings.ReplaceAll(id, "-", ""), "name": name})
				}
			}
		}
		json.NewEncoder(w).Encode(profiles)
	})
	mux.HandleFunc("/session/session/minecraft/profile/", func(w http.ResponseWriter, r *http.Request) {
		p.ProfileHits.Add(1)
		want := strings.TrimPrefix(r.URL.Path, "/session/session/minecraft/profile/")
		for name, id := range p.Profiles {
			if strings.ReplaceAll(id, "-", "") == want {
				json.NewEncoder(w).Encode(map[string]string{"id": want, "name": name})
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	base = p.srv.URL
	return p
}

// URL is where the fake serves.
func (p *Piston) URL() string { return p.srv.URL }

// Mojang points the given clients at the fake: the version index, the runtime index and the
// profile service.
func (p *Piston) Mojang(piston *mojang.Piston, runtimes *mojang.Runtimes, profiles *mojang.Profiles) {
	piston.ManifestURL = p.srv.URL + "/manifest.json"
	runtimes.IndexURL = p.srv.URL + "/jrt/all.json"
	profiles.APIURL = p.srv.URL + "/mojang"
	profiles.SessionURL = p.srv.URL + "/session"
}
