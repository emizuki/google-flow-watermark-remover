package watermark

import (
	"image"
	"image/draw"
	"math"
)

const (
	// opaqueAlpha: from this alpha up a pixel is treated as opaque. Inverting the blend there would amplify JPEG noise
	// beyond use, so the pixel is filled from its restored neighbours instead.
	opaqueAlpha = 0.92

	// edgeBandMin and edgeBandMax delimit the mark's edge band, alpha in [edgeBandMin, edgeBandMax). The core
	// (alpha ≥ edgeBandMax, flat at ~0.30–0.33) is left as inverted so that real texture survives there; smoothing the
	// core as well makes it flatter than its surroundings and leaves a ghost of the mark (residual correlation
	// 0.07–0.16 when only the band is filtered, 0.15–0.21 when the core is too).
	edgeBandMin = 0.02
	edgeBandMax = 0.25

	// chromaIterations is the number of Jacobi sweeps in fillEdgeChroma. The band is 2–4 px wide and has converged
	// long before this.
	chromaIterations = 200
)

// Remove inverts the white alpha blend of the mark: original = (observed − 255·a) / (1 − a), with a the mask alpha.
// It returns a new image and the number of pixels changed and of opaque pixels filled from neighbours. Pixels outside
// the mask are copied unchanged.
//
// The mask is applied at full strength. On every real Flow image with a known answer the mark matched the mask:
// strength 1.008 and 0.952 on a 2K/1K pair (each version shows the clean background under the other's mark), and ~1.0
// on a mark over white fabric, where full-strength removal leaves a correlation of 0.004. Fitting the strength per
// image from the surrounding texture did worse: 0.51 on that fabric image (a sharp background edge crosses the mark),
// and a mean error of 0.14 on twelve wood patches stamped at a known 0.95.
//
// After the inversion, the edge band gets a 3x3 median to remove JPEG ringing speckles. If img is a chroma-subsampled
// JPEG, the band's colour is then re-solved as well (see fillEdgeChroma).
func Remove(img image.Image, m *Mask) (out *image.RGBA, fixed, opaque int) {
	b := img.Bounds()
	out = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Bounds(), img, b.Min, draw.Src)
	var toFill [][2]int
	for y := range m.H {
		for x := range m.W {
			a := float64(m.Alpha[y*m.W+x])
			if a <= 0.02 {
				continue
			}
			px, py := m.X+x, m.Y+y
			if px < 0 || py < 0 || px >= out.Rect.Dx() || py >= out.Rect.Dy() {
				continue
			}
			fixed++
			if a >= opaqueAlpha {
				opaque++
				toFill = append(toFill, [2]int{px, py})
				continue
			}
			i := out.PixOffset(px, py)
			for k := range 3 {
				v := (float64(out.Pix[i+k]) - 255*a) / (1 - a)
				out.Pix[i+k] = uint8(math.Round(math.Max(0, math.Min(255, v))))
			}
		}
	}
	if len(toFill) > 0 {
		fillFromNeighbors(out, toFill)
	}
	medianEdgeBand(out, m)
	if yc, ok := img.(*image.YCbCr); ok && yc.SubsampleRatio != image.YCbCrSubsampleRatio444 {
		fillEdgeChroma(out, m)
	}
	return out, fixed, opaque
}

// fillFromNeighbors fills the given pixels with the mean of their already-restored 8-neighbours, working inwards over
// several passes until every pixel has at least three restored neighbours.
func fillFromNeighbors(out *image.RGBA, toFill [][2]int) {
	remaining := make(map[[2]int]bool, len(toFill))
	for _, p := range toFill {
		remaining[p] = true
	}
	for pass := 0; pass < 64 && len(remaining) > 0; pass++ {
		var done [][2]int
		updates := map[[2]int][3]float64{}
		for p := range remaining {
			var s [3]float64
			n := 0
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					q := [2]int{p[0] + dx, p[1] + dy}
					if (dx == 0 && dy == 0) || remaining[q] || q[0] < 0 || q[1] < 0 || q[0] >= out.Rect.Dx() || q[1] >= out.Rect.Dy() {
						continue
					}
					i := out.PixOffset(q[0], q[1])
					s[0] += float64(out.Pix[i])
					s[1] += float64(out.Pix[i+1])
					s[2] += float64(out.Pix[i+2])
					n++
				}
			}
			if n >= 3 {
				updates[p] = [3]float64{s[0] / float64(n), s[1] / float64(n), s[2] / float64(n)}
				done = append(done, p)
			}
		}
		if len(done) == 0 {
			return
		}
		for _, p := range done {
			i := out.PixOffset(p[0], p[1])
			v := updates[p]
			out.Pix[i], out.Pix[i+1], out.Pix[i+2] = uint8(v[0]+0.5), uint8(v[1]+0.5), uint8(v[2]+0.5)
			delete(remaining, p)
		}
	}
}

// medianEdgeBand applies a per-channel 3x3 median to the edge band, reading from a copy. The inversion cannot undo the
// ringing that JPEG left along the mark's sharp edge; a median removes the speckles and keeps real edges.
func medianEdgeBand(out *image.RGBA, m *Mask) {
	orig := append([]uint8(nil), out.Pix...)
	var window [9]uint8
	for y := range m.H {
		for x := range m.W {
			if a := float64(m.Alpha[y*m.W+x]); a < edgeBandMin || a >= edgeBandMax {
				continue
			}
			px, py := m.X+x, m.Y+y
			if px < 1 || py < 1 || px >= out.Rect.Dx()-1 || py >= out.Rect.Dy()-1 {
				continue
			}
			i := out.PixOffset(px, py)
			for k := range 3 {
				n := 0
				for dy := -1; dy <= 1; dy++ {
					for dx := -1; dx <= 1; dx++ {
						window[n] = orig[out.PixOffset(px+dx, py+dy)+k]
						n++
					}
				}
				out.Pix[i+k] = median9(window)
			}
		}
	}
}

// median9 returns the median of nine values (insertion sort on a copy, no allocation).
func median9(a [9]uint8) uint8 {
	for i := 1; i < 9; i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
	return a[4]
}

// fillEdgeChroma re-solves the colour (Cb, Cr) of the edge band, dilated by one pixel, as a harmonic interpolation
// (Laplace equation) between its two sides: the original background outside and the restored core inside. Luma is
// kept as inverted and median-filtered.
//
// Google compresses with 4:2:0 JPEG after stamping, so colour is stored at half resolution and smeared about 2 px
// across the white mark's edge. Inverting that with the sharp mask on each RGB channel leaves a coloured (orange on
// wood) outline around the old mark. Only colour is smoothed: it has already lost that detail to subsampling, while
// luma still carries real texture.
//
// Measured with the Pearson correlation between |∇channel| and |∇alpha| (0 = no outline; natural texture ±0.1):
//   - two real Flow images: Cb/Cr 0.74/0.69 → 0.20/0.07 (2752x1536) and 0.58/0.62 → −0.01/−0.03 (1376x768); the marked
//     originals were 0.31/0.33 and 0.27/0.23.
//   - twelve real wood patches stamped at strength 0.95 and saved as JPEG q92, where the original is known: PSNR over
//     the mark 38.34 → 38.62 dB, Cb/Cr 0.52 → 0.03. Smoothing luma in the band too erases grain: 37.69 dB, with some
//     patches 8 dB worse.
func fillEdgeChroma(out *image.RGBA, m *Mask) {
	inBand := func(x, y int) bool {
		if x < 0 || y < 0 || x >= m.W || y >= m.H {
			return false
		}
		a := float64(m.Alpha[y*m.W+x])
		return a >= edgeBandMin && a < edgeBandMax
	}
	index := make([]int, m.W*m.H) // mask-box pixel → index into band, or -1
	var band [][2]int
	for y := range m.H {
		for x := range m.W {
			index[y*m.W+x] = -1
			px, py := m.X+x, m.Y+y
			if px < 1 || py < 1 || px >= out.Rect.Dx()-1 || py >= out.Rect.Dy()-1 {
				continue
			}
			near := false
			for dy := -1; dy <= 1 && !near; dy++ {
				for dx := -1; dx <= 1 && !near; dx++ {
					near = inBand(x+dx, y+dy)
				}
			}
			if near {
				index[y*m.W+x] = len(band)
				band = append(band, [2]int{x, y})
			}
		}
	}
	if len(band) == 0 {
		return
	}
	// JFIF (BT.601 full range) in floating point, so values are rounded once at the end.
	chroma := func(x, y int) [2]float64 {
		i := out.PixOffset(m.X+x, m.Y+y)
		r, g, b := float64(out.Pix[i]), float64(out.Pix[i+1]), float64(out.Pix[i+2])
		return [2]float64{-0.168736*r - 0.331264*g + 0.5*b, 0.5*r - 0.418688*g - 0.081312*b}
	}
	cur := make([][2]float64, len(band))
	next := make([][2]float64, len(band))
	for j, p := range band {
		cur[j] = chroma(p[0], p[1])
	}
	neighbors := [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	for range chromaIterations {
		for j, p := range band {
			var sum [2]float64
			for _, d := range neighbors {
				x, y := p[0]+d[0], p[1]+d[1]
				var c [2]float64
				if x >= 0 && y >= 0 && x < m.W && y < m.H && index[y*m.W+x] >= 0 {
					c = cur[index[y*m.W+x]]
				} else {
					c = chroma(x, y)
				}
				sum[0], sum[1] = sum[0]+c[0], sum[1]+c[1]
			}
			next[j] = [2]float64{sum[0] / 4, sum[1] / 4}
		}
		cur, next = next, cur
	}
	clamp := func(v float64) uint8 { return uint8(math.Round(math.Max(0, math.Min(255, v)))) }
	for j, p := range band {
		i := out.PixOffset(m.X+p[0], m.Y+p[1])
		luma := 0.299*float64(out.Pix[i]) + 0.587*float64(out.Pix[i+1]) + 0.114*float64(out.Pix[i+2])
		cb, cr := cur[j][0], cur[j][1]
		out.Pix[i], out.Pix[i+1], out.Pix[i+2] = clamp(luma+1.402*cr), clamp(luma-0.344136*cb-0.714136*cr), clamp(luma+1.772*cb)
	}
}
