package loader

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	QuiltMetaURL  = "https://meta.quiltmc.org/v3"
	QuiltMavenURL = "https://maven.quiltmc.org/repository/release"
)

var quilt = Loader{
	Name: "quilt", Title: "Quilt", DependencyID: "quilt_loader",
	ComponentUID: "org.quiltmc.quilt-loader", MrpackKey: "quilt-loader", CurseForgeType: "5", GDLauncherType: "Quilt",
	AlsoRuns:        []string{"fabric"},
	ServerLaunchJar: "quilt-server-launch.jar",
	MetadataFiles:   []string{"quilt.mod.json", "fabric.mod.json"}, MarkerFile: "fabric.mod.json",
	TopLevelMandatory: true,
	versions:          quiltVersions, profile: quiltProfile, providesJar: quiltLoaderJarURL,
	ensureServer: quiltEnsureServer,
}

// quiltMeta reads Quilt's meta and Maven.
type quiltMeta struct {
	r        *Remote
	baseURL  string
	mavenURL string
}

func newQuiltMeta(r *Remote) quiltMeta {
	return quiltMeta{r: r, baseURL: r.url(QuiltMetaURL), mavenURL: r.url(QuiltMavenURL)}
}

// quiltVersions lists every Quilt loader for a game. Quilt meta has no stable flag and doesn't
// sort the list; a version without a prerelease suffix counts as stable.
func quiltVersions(ctx context.Context, r *Remote, game string) ([]Version, error) {
	q := newQuiltMeta(r)
	var entries []struct {
		Loader struct {
			Version string `json:"version"`
		} `json:"loader"`
	}
	if err := q.r.Fetch.GetJSON(ctx, q.baseURL+"/versions/loader/"+game, &entries); err != nil {
		return nil, fetchFailed(err, "quilt", "couldn't read the Quilt loaders for minecraft %s", game)
	}
	versions := make([]Version, 0, len(entries))
	for _, e := range entries {
		versions = append(versions, Version{Version: e.Loader.Version, Stable: !strings.Contains(e.Loader.Version, "-")})
	}
	return versions, nil
}

func quiltProfile(ctx context.Context, r *Remote, game, version string) (json.RawMessage, error) {
	q := newQuiltMeta(r)
	var raw json.RawMessage
	if err := q.r.Fetch.GetJSON(ctx, fmt.Sprintf("%s/versions/loader/%s/%s/profile/json", q.baseURL, game, version), &raw); err != nil {
		return nil, fetchFailed(err, "quilt", "couldn't read the Quilt %s profile for minecraft %s", version, game)
	}
	return raw, nil
}

// quiltLoaderJarURL points at the loader jar on Quilt's Maven. Quilt meta's loader hashes don't
// match the jars Maven serves, so callers hash the download themselves.
func quiltLoaderJarURL(ctx context.Context, r *Remote, game, version string) (string, error) {
	q := newQuiltMeta(r)
	var entry struct {
		Loader struct {
			Maven string `json:"maven"`
		} `json:"loader"`
	}
	if err := q.r.Fetch.GetJSON(ctx, fmt.Sprintf("%s/versions/loader/%s/%s", q.baseURL, game, version), &entry); err != nil {
		return "", fetchFailed(err, "quilt", "couldn't read Quilt loader %s for minecraft %s", version, game)
	}
	path, err := MavenPath(entry.Loader.Maven)
	if err != nil {
		return "", invalid("Quilt's meta gives no Maven coordinate for loader %s on minecraft %s", version, game)
	}
	return q.mavenURL + "/" + path, nil
}

type quiltLibrary struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

func (l quiltLibrary) jarURL() (string, error) {
	path, err := MavenPath(l.Name)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(l.URL, "/") + "/" + path, nil
}

// quiltServerProfile is what a Quilt server launches: its main classes and libraries.
type quiltServerProfile struct {
	MainClass         string         `json:"mainClass"`
	LauncherMainClass string         `json:"launcherMainClass"`
	Libraries         []quiltLibrary `json:"libraries"`
}

func (q quiltMeta) serverProfile(ctx context.Context, game, version string) (*quiltServerProfile, error) {
	var p quiltServerProfile
	if err := q.r.Fetch.GetJSON(ctx, fmt.Sprintf("%s/versions/loader/%s/%s/server/json", q.baseURL, game, version), &p); err != nil {
		return nil, fetchFailed(err, "quilt", "couldn't read the Quilt %s server profile for minecraft %s", version, game)
	}
	if p.MainClass == "" || p.LauncherMainClass == "" || len(p.Libraries) == 0 {
		return nil, invalid("the Quilt %s server profile for minecraft %s is incomplete", version, game)
	}
	return &p, nil
}
