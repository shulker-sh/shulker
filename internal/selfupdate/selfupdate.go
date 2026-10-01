// Package selfupdate finds, verifies and installs shulker's own releases from GitHub.
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"cmp"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"shulker.sh/shulker/internal/fetch"
)

// Releases reads shulker's releases on GitHub.
type Releases struct {
	Fetch       *fetch.Client
	LatestURL   string
	ListURL     string
	DownloadURL string
}

func New(f *fetch.Client) *Releases {
	return &Releases{
		Fetch:       f,
		LatestURL:   "https://api.github.com/repos/shulker-sh/shulker/releases/latest",
		ListURL:     "https://api.github.com/repos/shulker-sh/shulker/releases?per_page=30",
		DownloadURL: "https://github.com/shulker-sh/shulker/releases/download",
	}
}

// Published is a published release as GitHub's API describes it.
type Published struct {
	Tag string
	// Immutable is whether GitHub has locked the release: its tag can't move and its assets can't
	// be replaced, so the digests below are of the files it was published with.
	Immutable bool
	// Digests holds each asset's sha256 in hex, by file name.
	Digests map[string]string
}

type apiRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Immutable  bool   `json:"immutable"`
	Assets     []struct {
		Name   string `json:"name"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

func (rel apiRelease) published() Published {
	release := Published{Tag: rel.TagName, Immutable: rel.Immutable, Digests: map[string]string{}}
	for _, a := range rel.Assets {
		if sum, ok := strings.CutPrefix(a.Digest, "sha256:"); ok {
			release.Digests[a.Name] = sum
		}
	}
	return release
}

// Latest is the newest published release: the one GitHub calls latest, which is never a
// pre-release, or with pre the highest version among the newest releases, pre-releases included.
func (r *Releases) Latest(ctx context.Context, pre bool) (Published, error) {
	if pre {
		return r.latestOfAll(ctx)
	}
	var rel apiRelease
	if err := r.Fetch.GetJSON(ctx, r.LatestURL, &rel); err != nil {
		return Published{}, err
	}
	if rel.TagName == "" {
		return Published{}, fmt.Errorf("%s: no tag_name in the response", r.LatestURL)
	}
	return rel.published(), nil
}

func (r *Releases) latestOfAll(ctx context.Context) (Published, error) {
	var all []apiRelease
	if err := r.Fetch.GetJSON(ctx, r.ListURL, &all); err != nil {
		return Published{}, err
	}
	var newest *apiRelease
	for i, rel := range all {
		if rel.Draft || rel.TagName == "" {
			continue
		}
		if newest == nil || NeedsUpdate(newest.TagName, rel.TagName) {
			newest = &all[i]
		}
	}
	if newest == nil {
		return Published{}, fmt.Errorf("%s: %w", r.ListURL, fetch.ErrNotFound)
	}
	return newest.published(), nil
}

// AssetName is the release archive built for an OS and architecture.
func AssetName(tag, goos, goarch string) string {
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("shulker_%s_%s_%s.%s", strings.TrimPrefix(tag, "v"), goos, goarch, ext)
}

// ErrMutable is a release GitHub hasn't locked, whose assets could have been replaced since it
// was published.
var ErrMutable = errors.New("the release isn't immutable")

// Download saves this platform's archive for a release into dir and returns its path, once its
// sha256 matches the digest GitHub recorded when the release was published. A release that isn't
// immutable is refused, since its digests vouch for nothing.
func (r *Releases) Download(ctx context.Context, rel Published, dir string) (string, error) {
	if !rel.Immutable {
		return "", ErrMutable
	}
	asset := AssetName(rel.Tag, runtime.GOOS, runtime.GOARCH)
	want, ok := rel.Digests[asset]
	if !ok {
		return "", fmt.Errorf("the release lists no digest for %s", asset)
	}
	path := filepath.Join(dir, asset)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	_, err = r.Fetch.Download(ctx, r.url(rel.Tag, asset), io.MultiWriter(f, h))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, want) {
		return "", &ChecksumError{Asset: asset, Want: want, Got: got}
	}
	return path, nil
}

// ChecksumError is a downloaded archive whose sha256 isn't the one the release records for it.
type ChecksumError struct{ Asset, Want, Got string }

func (e *ChecksumError) Error() string {
	return fmt.Sprintf("checksum mismatch for %s (want %s, got %s)", e.Asset, e.Want, e.Got)
}

const bundleName = "shulker.attestation.jsonl"

// VerifyProvenance checks the archive against the release's build attestation with the gh CLI:
// that this repository's release workflow built it.
func (r *Releases) VerifyProvenance(ctx context.Context, tag, archive string) error {
	bundle := filepath.Join(filepath.Dir(archive), bundleName)
	f, err := os.Create(bundle)
	if err != nil {
		return err
	}
	_, err = r.Fetch.Download(ctx, r.url(tag, bundleName), f)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, "gh", "attestation", "verify", archive, "--bundle", bundle, "--repo", "shulker-sh/shulker", "--signer-workflow", "shulker-sh/shulker/.github/workflows/release.yml").CombinedOutput()
	if err != nil {
		return fmt.Errorf("gh attestation verify: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (r *Releases) url(tag, name string) string {
	return r.DownloadURL + "/" + tag + "/" + name
}

// NeedsUpdate reports whether latest is newer than current by semantic version precedence: a
// release is newer than its own pre-releases, and build metadata is ignored. A Dev build has no
// version to compare, and the answer for it is unknown rather than false.
func NeedsUpdate(current, latest string) bool {
	return compareVersions(latest, current) > 0
}

func compareVersions(a, b string) int {
	coreA, preA := splitVersion(a)
	coreB, preB := splitVersion(b)
	for i := 0; i < len(coreA) || i < len(coreB); i++ {
		var x, y int
		if i < len(coreA) {
			x = coreA[i]
		}
		if i < len(coreB) {
			y = coreB[i]
		}
		if x != y {
			return cmp.Compare(x, y)
		}
	}
	if len(preA) == 0 || len(preB) == 0 {
		return cmp.Compare(len(preB), len(preA))
	}
	for i := 0; i < len(preA) && i < len(preB); i++ {
		if c := comparePrerelease(preA[i], preB[i]); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(preA), len(preB))
}

// comparePrerelease orders two pre-release identifiers: numbers by value and below any word,
// words by their bytes.
func comparePrerelease(a, b string) int {
	x, errA := strconv.Atoi(a)
	y, errB := strconv.Atoi(b)
	switch {
	case errA == nil && errB == nil:
		return cmp.Compare(x, y)
	case errA == nil:
		return -1
	case errB == nil:
		return 1
	}
	return strings.Compare(a, b)
}

// splitVersion is a version's numbers and its pre-release identifiers, none for a release.
func splitVersion(v string) (core []int, pre []string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v, _, _ = strings.Cut(v, "+")
	v, suffix, isPre := strings.Cut(v, "-")
	for _, p := range strings.Split(v, ".") {
		n, _ := strconv.Atoi(p)
		core = append(core, n)
	}
	if isPre {
		pre = strings.Split(suffix, ".")
	}
	return core, pre
}

// Executable is the running binary's path with symlinks resolved, so an install replaces the file
// itself and not a link to it.
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// Install replaces exe with the binary inside archive.
func Install(archive, exe string) error {
	staged := exe + ".new"
	if err := extractBinary(archive, staged); err != nil {
		os.Remove(staged)
		return err
	}
	if runtime.GOOS != "windows" {
		if err := os.Rename(staged, exe); err != nil {
			os.Remove(staged)
			return err
		}
		return nil
	}
	// Windows won't overwrite or delete a running exe, but it will rename one. The
	// renamed copy is removed on the next run (RemoveOld).
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		os.Remove(staged)
		return err
	}
	if err := os.Rename(staged, exe); err != nil {
		os.Rename(old, exe)
		return err
	}
	return nil
}

// RemoveOld deletes the copy a Windows install renamed aside, which couldn't be deleted while it ran.
func RemoveOld() {
	if exe, err := Executable(); err == nil {
		os.Remove(exe + ".old")
	}
}

func extractBinary(archive, dest string) error {
	if strings.HasSuffix(archive, ".zip") {
		return extractZip(archive, "shulker.exe", dest)
	}
	return extractTarGz(archive, "shulker", dest)
}

func extractTarGz(archive, name, dest string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%s: no %s inside", filepath.Base(archive), name)
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == name {
			return writeExecutable(dest, tr)
		}
	}
}

func extractZip(archive, name, dest string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if filepath.Base(zf.Name) != name || zf.FileInfo().IsDir() {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		defer rc.Close()
		return writeExecutable(dest, rc)
	}
	return fmt.Errorf("%s: no %s inside", filepath.Base(archive), name)
}

func writeExecutable(dest string, r io.Reader) error {
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
