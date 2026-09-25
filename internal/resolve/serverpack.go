package resolve

import (
	"archive/zip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path"
	"slices"
	"strings"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/provider"
)

// ServerPack is what the server files a pack pairs with decided about the imported mods' sides.
type ServerPack struct {
	// Client are the mods the server files leave out, now client-only by requires.
	Client []string `json:"client"`
	// Both are the mods the server files ship that were client-only by their own metadata.
	Both []string `json:"both"`
}

// ReadServerPack finds the pack archive on the manifest's providers and, when its version pairs
// server files with it, lists their mods folder by ranged reads rather than downloading it. A mod
// the server files leave out is client-only, and one they ship that its metadata made client-only
// goes on both sides, each written into requires, so the pack's choice outlives an update. Mods whose requires already names a
// side keep it. It is nil when no provider hosts the archive or pairs server files with it.
func (r *Resolver) ReadServerPack(ctx context.Context, name string, archive []byte) (*ServerPack, error) {
	for _, providerName := range r.Manifest.ProviderOrder() {
		p, err := r.Providers.Get(providerName)
		if err != nil {
			continue
		}
		r.log("looking the pack up on %s", p.Title())
		v, err := findPack(ctx, p, name, archive)
		if err != nil {
			return nil, err
		}
		if v == nil {
			continue
		}
		if v.ServerPack == "" {
			return nil, nil
		}
		server, err := p.Version(ctx, v.ServerPack)
		if err != nil {
			return nil, err
		}
		r.log("reading the file list of %s", server.File.Filename)
		f, err := r.Fetch.Remote(ctx, server.File.URL)
		if err != nil {
			return nil, err
		}
		zr, err := zip.NewReader(f, f.Size())
		if err != nil {
			return nil, fmt.Errorf("%s: %w", server.File.Filename, err)
		}
		jars := serverJars(zr.File)
		if len(jars) == 0 {
			return nil, fmt.Errorf("%s has no mods folder", server.File.Filename)
		}
		return r.applyServerPack(jars), nil
	}
	return nil, nil
}

// findPack is the version p hosts archive as. A provider may leave modpacks out of its hash
// index, as CurseForge's fingerprints do, so a miss there searches its modpacks by the pack's
// name for a file with the archive's sha1.
func findPack(ctx context.Context, p provider.Provider, name string, archive []byte) (*provider.Version, error) {
	found, err := p.Identify(ctx, map[string][]byte{"pack": archive})
	if err != nil {
		return nil, err
	}
	if h, ok := found["pack"]; ok {
		return &h.Version, nil
	}
	if name == "" {
		return nil, nil
	}
	sum := sha1.Sum(archive)
	want := hex.EncodeToString(sum[:])
	hits, err := p.Search(ctx, name, manifest.TypeModpack, 5)
	if err != nil {
		return nil, err
	}
	for _, hit := range hits {
		versions, err := p.Versions(ctx, hit.ID, "", nil)
		if err != nil {
			return nil, err
		}
		for _, v := range versions {
			if v.File.Sha1 == want {
				return &v, nil
			}
		}
	}
	return nil, nil
}

// serverJars are the jar names in the archive's outermost mods folder. A server pack may sit
// inside a folder of its own, and a mod's config may hold a mods folder deeper down.
func serverJars(files []*zip.File) map[string]bool {
	byDepth := map[int]map[string]bool{}
	for _, f := range files {
		dir, name := path.Split(f.Name)
		if path.Base(dir) != "mods" || !strings.HasSuffix(name, ".jar") {
			continue
		}
		depth := strings.Count(dir, "/")
		if byDepth[depth] == nil {
			byDepth[depth] = map[string]bool{}
		}
		byDepth[depth][name] = true
	}
	if len(byDepth) == 0 {
		return nil
	}
	return byDepth[slices.Min(slices.Collect(maps.Keys(byDepth)))]
}

func (r *Resolver) serverPackOf(ctx context.Context, arc *packarchive.Archive) (*ServerPack, error) {
	data, err := os.ReadFile(arc.Path)
	if err != nil {
		return nil, err
	}
	return r.ReadServerPack(ctx, arc.Name, data)
}

func (r *Resolver) applyServerPack(jars map[string]bool) *ServerPack {
	sp := &ServerPack{Client: []string{}, Both: []string{}}
	for _, id := range slices.Sorted(maps.Keys(r.Lock.Mods)) {
		m := r.Lock.Mods[id]
		req, listed := r.Manifest.Requires[id]
		if !listed || req.Side != "" {
			continue
		}
		switch shipped := jars[m.Filename]; {
		case !shipped && m.Side != "server":
			req.Side = "client"
			sp.Client = append(sp.Client, id)
		case shipped && m.Side == "client":
			req.Side = "both"
			sp.Both = append(sp.Both, id)
		default:
			continue
		}
		r.Manifest.Requires[id] = req
		m.Side, m.SideFrom = req.Side, sideFromRequires
		r.Lock.Mods[id] = m
	}
	return sp
}
