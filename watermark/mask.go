package watermark

import (
	"bytes"
	_ "embed"
	"image/color"
	"image/png"
	"math"
	"sync"
)

// The mark is one fixed stamp: a 58x58 alpha map with peak alpha ≈ 0.33, measured by averaging five dark Flow images
// (one per aspect ratio) aligned within ±3 px, each correlating ≥ 0.985 with the first. The same stamp, unscaled,
// appears on 2752x1536 images.
//
//go:embed sparkle.png
var stampPNG []byte

// Mask is the alpha map of the mark as placed on an image of one specific size. The region (X, Y, W, H) is measured in
// pixels from the top-left corner of the image; Alpha holds W*H values in [0, 1], row-major.
type Mask struct {
	Width, Height int
	X, Y, W, H    int
	Alpha         []float32
}

type stamp struct {
	w, h  int
	alpha []float32
}

// loadStamp decodes the embedded stamp once. The file is compiled in, so a decode error is a build defect.
var loadStamp = sync.OnceValue(func() stamp {
	img, err := png.Decode(bytes.NewReader(stampPNG))
	if err != nil {
		panic("watermark: embedded sparkle.png: " + err.Error())
	}
	b := img.Bounds()
	st := stamp{w: b.Dx(), h: b.Dy(), alpha: make([]float32, b.Dx()*b.Dy())}
	for y := range st.h {
		for x := range st.w {
			g := color.GrayModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.Gray)
			st.alpha[y*st.w+x] = float32(g.Y) / 255
		}
	}
	return st
})

// cornerMargin is the gap in pixels between the mark's box and both the right and the bottom edge. The gap is the same
// on both sides and grows with the image's shorter side; 52 + short/48 reproduces every measured placement:
//
//	1376x768, 768x1376 → 68    1200x896, 896x1200 → 71    1024x1024 → 73    2752x1536 → 84
//
// For a size that has not been measured this is a prediction. If it is wrong the mark does not line up, Score stays
// low and the image is left unchanged.
func cornerMargin(width, height int) int {
	return int(math.Round(52 + float64(min(width, height))/48))
}

// MaskFor returns the mask placed for an image of the given size, or false if the image is too small to hold it.
func MaskFor(width, height int) (*Mask, bool) {
	st := loadStamp()
	margin := cornerMargin(width, height)
	x, y := width-margin-st.w, height-margin-st.h
	if x < ringWidth || y < ringWidth {
		return nil, false
	}
	return stampAt(width, height, x, y), true
}

// stampAt returns the stamp placed with its top-left corner at (x, y) on an image of the given size.
func stampAt(width, height, x, y int) *Mask {
	st := loadStamp()
	return &Mask{
		Width: width, Height: height,
		X: x, Y: y, W: st.w, H: st.h,
		Alpha: append([]float32(nil), st.alpha...), // callers may modify their copy
	}
}

// otherMark is a star that Google's other image apps stamp instead of the Flow mark: the same four-pointed shape,
// star pixels wide, with its star box gap pixels from the right and bottom edge.
type otherMark struct{ star, gap int }

// otherMarks are the placements seen on images from Google's other apps (catalogued by an independent project's
// public samples, where masks built this way score up to 1.00 on the real marks). On a 2752x1536 image the 96 px star
// shares its centre with the Flow mark, and a 48 px star 96 px from the corner can sit where texture happens to
// resemble the Flow mark; removing the Flow mask there damages the image, so these are checked before removal.
var otherMarks = []otherMark{{48, 32}, {48, 96}, {96, 64}, {96, 192}}

type otherPlacement struct {
	otherMark
	mask *Mask
}

// otherMasks returns the other-app marks that fit on an image of the given size, built from the Flow stamp: its star
// is 48 px wide inside the box, so a 96 px star is the stamp scaled by two.
func otherMasks(width, height int) []otherPlacement {
	st := loadStamp()
	var out []otherPlacement
	for _, o := range otherMarks {
		box := int(math.Round(float64(st.w) * float64(o.star) / 48))
		pad := (box - o.star) / 2
		x, y := width-(o.gap-pad)-box, height-(o.gap-pad)-box
		if x < ringWidth || y < ringWidth {
			continue
		}
		m := &Mask{Width: width, Height: height, X: x, Y: y, W: box, H: box, Alpha: st.scaled(box)}
		out = append(out, otherPlacement{o, m})
	}
	return out
}

// scaled resamples the stamp's alpha to size x size pixels (bilinear).
func (st stamp) scaled(size int) []float32 {
	if size == st.w {
		return append([]float32(nil), st.alpha...)
	}
	at := func(x, y int) float64 {
		x, y = max(0, min(st.w-1, x)), max(0, min(st.h-1, y))
		return float64(st.alpha[y*st.w+x])
	}
	f := float64(st.w) / float64(size)
	out := make([]float32, size*size)
	for y := range size {
		for x := range size {
			sx, sy := (float64(x)+0.5)*f-0.5, (float64(y)+0.5)*f-0.5
			x0, y0 := int(math.Floor(sx)), int(math.Floor(sy))
			fx, fy := sx-float64(x0), sy-float64(y0)
			v := at(x0, y0)*(1-fx)*(1-fy) + at(x0+1, y0)*fx*(1-fy) + at(x0, y0+1)*(1-fx)*fy + at(x0+1, y0+1)*fx*fy
			out[y*size+x] = float32(v)
		}
	}
	return out
}
