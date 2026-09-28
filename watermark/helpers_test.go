package watermark

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
)

// syntheticMask is a soft four-pointed star (like the ✦) of 48x48 with peak alpha amax, 60 px from the bottom-right
// corner of a width x height image.
func syntheticMask(width, height int, amax float32) *Mask {
	const w, h = 48, 48
	m := &Mask{Width: width, Height: height, X: width - 60 - w, Y: height - 60 - h, W: w, H: h, Alpha: make([]float32, w*h)}
	for y := range h {
		for x := range w {
			dx := math.Abs(float64(x)-23.5) / 24
			dy := math.Abs(float64(y)-23.5) / 24
			a := max(0, 1-(math.Sqrt(dx)+math.Sqrt(dy)))
			m.Alpha[y*w+x] = amax * float32(math.Min(1, a*1.6))
		}
	}
	return m
}

// randomImage is a colour gradient with per-channel noise, so the inversion has detail to recover.
func randomImage(width, height int, seed uint64) *image.RGBA {
	r := rand.New(rand.NewPCG(seed, 0))
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			i := img.PixOffset(x, y)
			img.Pix[i] = uint8(40 + x*150/width + r.IntN(20))
			img.Pix[i+1] = uint8(60 + y*120/height + r.IntN(20))
			img.Pix[i+2] = uint8(30 + (x+y)*90/(width+height) + r.IntN(20))
			img.Pix[i+3] = 255
		}
	}
	return img
}

// woodImage is a warm textured background with diagonal light bands around the given brightness level.
func woodImage(width, height int, level float64) *image.RGBA {
	img := randomImage(width, height, 21)
	for y := range height {
		for x := range width {
			i := img.PixOffset(x, y)
			base := level + 25*math.Sin(float64(x+y)/37)
			img.Pix[i] = uint8(math.Min(255, base+20+float64(img.Pix[i]%12)))
			img.Pix[i+1] = uint8(math.Min(255, base+float64(img.Pix[i+1]%12)))
			img.Pix[i+2] = uint8(math.Min(255, base-25+float64(img.Pix[i+2]%12)))
		}
	}
	return img
}

// stampImage blends a white mark with alpha = strength × mask into a copy of orig, as Google does.
func stampImage(orig *image.RGBA, m *Mask, strength float64) *image.RGBA {
	out := image.NewRGBA(orig.Rect)
	copy(out.Pix, orig.Pix)
	for y := range m.H {
		for x := range m.W {
			a := float64(m.Alpha[y*m.W+x]) * strength
			i := out.PixOffset(m.X+x, m.Y+y)
			for k := range 3 {
				out.Pix[i+k] = uint8(math.Round(float64(out.Pix[i+k])*(1-a) + 255*a))
			}
		}
	}
	return out
}

// psnr compares a and b over the mask region.
func psnr(a, b *image.RGBA, m *Mask) float64 {
	var se float64
	n := 0
	for y := range m.H {
		for x := range m.W {
			i := a.PixOffset(m.X+x, m.Y+y)
			j := b.PixOffset(m.X+x, m.Y+y)
			for k := range 3 {
				d := float64(a.Pix[i+k]) - float64(b.Pix[j+k])
				se += d * d
				n++
			}
		}
	}
	if se == 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10(255*255/(se/float64(n)))
}

// edgeCorrelation is the Pearson correlation between the gradient magnitude of one YCbCr channel of img (0 = Y,
// 1 = Cb, 2 = Cr) and that of the mask alpha, over the mask box. Near 0 the mark's outline is invisible in that
// channel; a high value means an outline in the mark's shape is left.
func edgeCorrelation(img image.Image, m *Mask, ch int) float64 {
	val := func(x, y int) float64 {
		r, g, b, _ := img.At(x, y).RGBA()
		yy, cb, cr := color.RGBToYCbCr(uint8(r>>8), uint8(g>>8), uint8(b>>8))
		return float64([3]uint8{yy, cb, cr}[ch])
	}
	var sx, sy, sxx, syy, sxy, n float64
	for y := 1; y < m.H-1; y++ {
		for x := 1; x < m.W-1; x++ {
			ga := math.Hypot(float64(m.Alpha[y*m.W+x+1]-m.Alpha[y*m.W+x-1]), float64(m.Alpha[(y+1)*m.W+x]-m.Alpha[(y-1)*m.W+x]))
			px, py := m.X+x, m.Y+y
			gi := math.Hypot(val(px+1, py)-val(px-1, py), val(px, py+1)-val(px, py-1))
			sx, sy, sxx, syy, sxy, n = sx+gi, sy+ga, sxx+gi*gi, syy+ga*ga, sxy+gi*ga, n+1
		}
	}
	return (sxy - sx*sy/n) / math.Sqrt((sxx-sx*sx/n)*(syy-sy*sy/n))
}

func writePNG(t *testing.T, dir, name string, img image.Image) string {
	t.Helper()
	p := filepath.Join(dir, name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	return p
}

func readRGBA(t *testing.T, p string) *image.RGBA {
	t.Helper()
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	out := image.NewRGBA(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Src)
	return out
}
