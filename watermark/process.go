// Package watermark removes the visible "✦" mark that Google stamps into the bottom-right corner of images generated
// in Google Flow (Nano Banana, Nano Banana Pro).
//
// The mark is a white stamp alpha-blended with a fixed mask: observed = original·(1 − a) + 255·a. With the mask known
// per pixel, the original is recovered exactly as original = (observed − 255·a) / (1 − a): nothing is painted over,
// cropped or guessed. The stamp is the same at every image size; only its distance from the corner changes, and that
// distance follows from the image size (see MaskFor).
//
// Removal is guarded: the mark must be detected at its known position (Score ≥ threshold), and a result that turns
// darker than its surroundings in the mark's shape (an image that was already clean) is rejected.
package watermark

import (
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	// DefaultThreshold is the minimum Score to treat an image of unknown origin as marked. On real images the mark
	// scored 0.20–0.98 at its position while unmarked positions reached 0.42, so no threshold separates them cleanly;
	// images known to come from Flow can use FlowThreshold.
	DefaultThreshold = 0.2

	// FlowThreshold is for images known to come from Flow, which always stamps the mark: it only has to reject flat
	// or unmarked images (Score ~0).
	FlowThreshold = 0.1

	// JPEGQuality is used when re-encoding a JPEG after removal (Flow serves ~92; 95 avoids losing more detail).
	JPEGQuality = 95
)

// inverseLimit: if the correlation after removal drops below −inverseLimit, the region is now darker than its ring in
// the shape of the mark, so there was no mark to remove (an already-cleaned image lands near −0.9; a real mark leaves
// 0 to 0.2).
const inverseLimit = 0.3

// Status is the outcome of processing one image.
type Status int

const (
	Removed   Status = iota // the mark was found and removed
	TooSmall                // the image is too small to carry the mark
	NoMark                  // the mark was not detected at its position
	NotAMark                // removal would print a dark mark: the image is already clean
	OtherMark               // the corner carries a mark from another Google app, which this package does not remove
)

func (s Status) String() string {
	switch s {
	case Removed:
		return "removed"
	case TooSmall:
		return "image too small"
	case NoMark:
		return "no mark detected"
	case NotAMark:
		return "already clean"
	case OtherMark:
		return "not a Google Flow mark"
	}
	return fmt.Sprintf("Status(%d)", int(s))
}

// Result describes what Process did and why.
type Result struct {
	Status Status
	Width  int
	Height int
	Score  float64 // detection score at the mark's position, see Score
	Fixed  int     // pixels changed
	Opaque int     // pixels filled from neighbours instead of inverted
	Detail string  // explanation with the measured values when Status is not Removed
}

// Process detects and removes the mark. It returns the cleaned image, or nil when the image is left unchanged, in
// which case Result.Status and Result.Detail say why. The input image is never modified.
func Process(img image.Image, threshold float64) (*image.RGBA, Result) {
	b := img.Bounds()
	res := Result{Width: b.Dx(), Height: b.Dy()}
	m, ok := MaskFor(b.Dx(), b.Dy())
	if !ok {
		res.Status = TooSmall
		res.Detail = fmt.Sprintf("%dx%d is too small to carry the mark", b.Dx(), b.Dy())
		return nil, res
	}
	res.Score = Score(img, m)
	if res.Score < threshold {
		res.Status = NoMark
		res.Detail = fmt.Sprintf("no mark in the corner (score %.3f < %.2f)", res.Score, threshold)
		return nil, res
	}
	best, bestScore := otherMark{}, res.Score
	for _, o := range otherMasks(b.Dx(), b.Dy()) {
		if s := Score(img, o.mask); s > bestScore {
			best, bestScore = o.otherMark, s
		}
	}
	if bestScore > res.Score {
		res.Status = OtherMark
		res.Detail = fmt.Sprintf("the corner carries a %d px mark %d px from the edge, as other Google apps stamp (score %.2f > %.2f); only Google Flow marks are removed", best.star, best.gap, bestScore, res.Score)
		return nil, res
	}
	out, fixed, opaque := Remove(img, m)
	res.Fixed, res.Opaque = fixed, opaque
	if r := correlation(out, m); r < -inverseLimit {
		res.Status = NotAMark
		res.Detail = fmt.Sprintf("removal would leave a dark mark (correlation %.2f); the image is already clean or its texture matches the shape", r)
		return nil, res
	}
	res.Status = Removed
	return out, res
}

// ProcessFile runs Process on a JPEG or PNG file and overwrites it in place when the mark was removed. The file is
// left untouched otherwise. The write goes through a temporary file and a rename.
func ProcessFile(path string, threshold float64) (Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return Result{}, err
	}
	img, format, err := image.Decode(f)
	_ = f.Close()
	if err != nil {
		return Result{}, fmt.Errorf("decode %s: %w", path, err)
	}
	out, res := Process(img, threshold)
	if out == nil {
		return res, nil
	}
	if err := writeFile(path, format, out); err != nil {
		return res, err
	}
	return res, nil
}

// Encode writes img in the given format: "png", or "jpeg" at JPEGQuality.
func Encode(w io.Writer, format string, img image.Image) error {
	switch strings.ToLower(format) {
	case "png":
		return png.Encode(w, img)
	case "jpeg", "jpg":
		return jpeg.Encode(w, img, &jpeg.Options{Quality: JPEGQuality})
	default:
		return errors.New("cannot encode format " + format)
	}
}

func writeFile(path, format string, img image.Image) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	err = Encode(f, format, img)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, filepath.Clean(path))
}
