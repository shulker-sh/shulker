package launcher

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/png"
	"path/filepath"

	"golang.org/x/image/draw"
	"shulker.sh/shulker/internal/fsutil"
)

// Image is the picture a launcher shows for an instance, and how a pack icon becomes it.
type Image struct {
	File    string
	Default []byte
	fit     func(icon []byte) ([]byte, error)
}

// SyncImage writes the launcher's image for the instance holding gameDir: the pack icon fitted to
// the launcher, or the default when icon is nil. last is the hash of the image written before, ""
// standing for the default the link wrote, and an image matching it is left alone so one the
// player picked survives. It returns the hash to record.
func (e *Entry) SyncImage(gameDir string, icon []byte, last string) (string, error) {
	im := e.Image
	if im == nil {
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
	return hash, fsutil.Write(filepath.Join(e.InstanceDir(gameDir), im.File), want)
}

func imageHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// atlauncherCard fits an icon, square, into the middle of a transparent 300×150 canvas, since
// ATLauncher stretches any card image to that size.
func atlauncherCard(icon []byte) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(icon))
	if err != nil {
		return nil, err
	}
	const width, height = 300, 150
	b := src.Bounds()
	scale := min(float64(height)/float64(b.Dx()), float64(height)/float64(b.Dy()))
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
