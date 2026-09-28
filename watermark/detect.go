package watermark

import (
	"image"
	"image/color"
	"image/draw"
	"math"
)

// ringWidth is the width in pixels of the band just outside the mask box that serves as the local background.
const ringWidth = 6

func brightness(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	return float64(r+g+b) / 3 / 257
}

// Score reports how strongly the mask region looks like the mark, in [0, 1]: the Pearson correlation between the
// brightness above the local background and the mask alpha. A present mark reproduces the mask's shape whether the
// background is light or dark, flat or textured; an unmarked region scores near 0.
func Score(img image.Image, m *Mask) float64 {
	return math.Max(0, math.Min(1, correlation(img, m)))
}

// correlation is the signed form of Score, in [-1, 1]. A clearly negative value means the region is darker than its
// surroundings in the shape of the mark, which only happens when a clean image has been "un-blended" (a second removal)
// or when texture happens to match the shape.
func correlation(img image.Image, m *Mask) float64 {
	b := img.Bounds()
	sample := func(x, y int) (float64, bool) {
		px, py := b.Min.X+x, b.Min.Y+y
		if px < b.Min.X || py < b.Min.Y || px >= b.Max.X || py >= b.Max.Y {
			return 0, false
		}
		return brightness(img.At(px, py)), true
	}
	var bg float64
	bgCount := 0
	for y := m.Y - ringWidth; y < m.Y+m.H+ringWidth; y++ {
		for x := m.X - ringWidth; x < m.X+m.W+ringWidth; x++ {
			if x >= m.X && y >= m.Y && x < m.X+m.W && y < m.Y+m.H {
				continue
			}
			if v, ok := sample(x, y); ok {
				bg += v
				bgCount++
			}
		}
	}
	if bgCount == 0 {
		return 0
	}
	bg /= float64(bgCount)
	var sx, sy, sxx, syy, sxy float64
	n := 0
	for y := range m.H {
		for x := range m.W {
			v, ok := sample(m.X+x, m.Y+y)
			if !ok {
				continue
			}
			e := v - bg
			a := float64(m.Alpha[y*m.W+x])
			sx += e
			sy += a
			sxx += e * e
			syy += a * a
			sxy += e * a
			n++
		}
	}
	if n < 2 {
		return 0
	}
	fn := float64(n)
	cov := sxy - sx*sy/fn
	vx := sxx - sx*sx/fn
	vy := syy - sy*sy/fn
	if vx <= 0 || vy <= 0 {
		return 0
	}
	return cov / math.Sqrt(vx*vy)
}

// locateMargin bounds the search of Locate: the mark's box is looked for between 0 and locateMargin pixels from the
// right and bottom edges. Measured boxes sit 68–84 px from the corner.
const locateMargin = 200

// Locate searches the bottom-right corner of img for the best match of the stamp, to check the placement MaskFor
// predicts for a new image size. It returns the stamp placed at that position and its Score.
//
// Searching tens of thousands of positions finds texture that happens to resemble the stamp: unmarked corners of real
// photos reached 0.63, higher than some real marks (0.62). A high score alone therefore does not prove a mark; a match
// at the predicted position does.
func Locate(img image.Image) (*Mask, float64) {
	b := img.Bounds()
	probe := stampAt(b.Dx(), b.Dy(), 0, 0)
	best := stampAt(b.Dx(), b.Dy(), 0, 0)
	// Work on an RGBA copy of the corner with origin (0, 0): much faster to sample than a decoded YCbCr image.
	span := locateMargin + probe.W + 2*ringWidth
	corner := image.Rect(max(0, b.Dx()-span), max(0, b.Dy()-span), b.Dx(), b.Dy())
	crop := image.NewRGBA(image.Rect(0, 0, corner.Dx(), corner.Dy()))
	draw.Draw(crop, crop.Bounds(), img, b.Min.Add(corner.Min), draw.Src)
	bestScore := -2.0
	for py := max(0, crop.Rect.Dy()-probe.H-locateMargin); py <= crop.Rect.Dy()-probe.H; py++ {
		for px := max(0, crop.Rect.Dx()-probe.W-locateMargin); px <= crop.Rect.Dx()-probe.W; px++ {
			probe.X, probe.Y = px, py
			if c := correlation(crop, probe); c > bestScore {
				bestScore, best.X, best.Y = c, corner.Min.X+px, corner.Min.Y+py
			}
		}
	}
	return best, math.Max(0, bestScore)
}
