package launcher

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/fsutil"
)

// Icon is shulker's own icon, 128px square: the picture an instance gets when its pack names none,
// in a launcher that shows a square one of any size.
//
//go:embed assets/icon.png
var Icon []byte

// Image is the picture a launcher shows for an instance, and how a pack icon becomes it.
type Image struct {
	// File is the image's file in the instance directory, for a launcher that keeps one there.
	File    string
	Default []byte
	fit     func(icon []byte) ([]byte, error)
	// write puts the image where the launcher reads it, for a launcher that keeps none in the
	// instance directory. It works from the launcher directory, so a row that records none is
	// left alone.
	write func(e *Entry, in config.Instance, image []byte) error
}

// SyncImage writes the launcher's image for the instance in: the pack icon fitted to the launcher,
// or the default when icon is nil. last is the hash of the image written before, "" standing for
// the default the link wrote, and an image matching it is left alone so one the player picked
// survives. It returns the hash to record.
func (e *Entry) SyncImage(in config.Instance, icon []byte, last string) (string, error) {
	im := e.Image
	if im == nil || im.write != nil && in.LauncherDir == "" {
		return last, nil
	}
	want := im.Default
	if icon != nil {
		want = icon
		if im.fit != nil {
			var err error
			if want, err = im.fit(icon); err != nil {
				return last, err
			}
		}
	}
	hash := imageHash(want)
	if last == "" {
		last = imageHash(im.Default)
	}
	if hash == last {
		return hash, nil
	}
	if im.write != nil {
		return hash, im.write(e, in, want)
	}
	return hash, fsutil.Write(filepath.Join(e.InstanceDir(in.Dir), im.File), want)
}

// writeIconFile writes an image into a launcher's icons folder, which a launcher that has never
// shown a custom icon may not have made yet.
func writeIconFile(path string, image []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsutil.Write(path, image)
}

// writeDefaultIcon writes a link's default image at path, unless an image is already there.
func writeDefaultIcon(path string) error {
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return writeIconFile(path, Icon)
}

// keepsIconKey reports whether an instance.cfg iconKey is one the player picked, which a link
// leaves alone; "default" is what Prism and MultiMC write for an instance with no icon chosen.
func keepsIconKey(value string) bool {
	return value != "" && value != "default"
}

func imageHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// atlauncherCard fits an icon, square, into the middle of a transparent 300×150 canvas, since
// ATLauncher stretches any card image to that size.
func atlauncherCard(icon []byte) ([]byte, error) {
	return fitCanvas(icon, 150, 300, 150)
}

// fitCanvas scales an icon to fit a square of side pixels, keeping its shape, and centres it on a
// transparent canvas.
func fitCanvas(icon []byte, side, width, height int) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(icon))
	if err != nil {
		return nil, err
	}
	b := src.Bounds()
	scale := min(float64(side)/float64(b.Dx()), float64(side)/float64(b.Dy()))
	w, h := max(1, int(float64(b.Dx())*scale+0.5)), max(1, int(float64(b.Dy())*scale+0.5))
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	at := image.Pt((width-w)/2, (height-h)/2)
	draw.CatmullRom.Scale(dst, image.Rectangle{Min: at, Max: at.Add(image.Pt(w, h))}, src, b, draw.Over, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
