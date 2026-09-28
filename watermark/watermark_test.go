package watermark

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The corner margin formula must reproduce every placement measured on real Flow images, and the stamp must be the
// measured 58x58 map with its ~0.33 core.
func TestMaskForMatchesMeasuredPlacements(t *testing.T) {
	measured := []struct{ width, height, x, y int }{
		{1376, 768, 1250, 642},
		{768, 1376, 642, 1250},
		{1024, 1024, 893, 893},
		{1200, 896, 1071, 767},
		{896, 1200, 767, 1071},
		{2752, 1536, 2610, 1394},
		{1792, 2400, 1645, 2253},
	}
	for _, p := range measured {
		m, ok := MaskFor(p.width, p.height)
		if !ok {
			t.Fatalf("%dx%d: no mask", p.width, p.height)
		}
		if m.X != p.x || m.Y != p.y {
			t.Errorf("%dx%d: mask at %d,%d, measured %d,%d", p.width, p.height, m.X, m.Y, p.x, p.y)
		}
	}
	m, _ := MaskFor(1376, 768)
	if m.W != 58 || m.H != 58 || len(m.Alpha) != 58*58 {
		t.Fatalf("stamp is %dx%d with %d alpha values, want 58x58", m.W, m.H, len(m.Alpha))
	}
	if peak := slices.Max(m.Alpha); peak < 0.30 || peak > 0.36 {
		t.Errorf("stamp peak alpha %.3f, want ~0.33", peak)
	}
	if _, ok := MaskFor(100, 100); ok {
		t.Error("a 100x100 image cannot hold the mark with its margin")
	}
}

// Inverting the blend restores the original almost exactly where the mark is translucent; opaque parts are filled
// from neighbours and must at least leave no bright trace. An unmarked image does not score as marked.
func TestRemoveRestoresOriginal(t *testing.T) {
	orig := randomImage(320, 200, 7)
	for _, amax := range []float32{0.5, 0.75, 1.0} {
		m := syntheticMask(320, 200, amax)
		marked := stampImage(orig, m, 1)
		if s := Score(marked, m); s < DefaultThreshold {
			t.Errorf("amax %.2f: marked image scores %.3f, want ≥ %.2f", amax, s, DefaultThreshold)
		}
		if s := Score(orig, m); s >= DefaultThreshold {
			t.Errorf("amax %.2f: unmarked image scores %.3f, want < %.2f", amax, s, DefaultThreshold)
		}
		out, fixed, opaque := Remove(marked, m)
		want := 40.0
		if amax >= opaqueAlpha {
			want = 28 // opaque pixels cannot be recovered, only filled
		}
		if p := psnr(out, orig, m); p < want {
			t.Errorf("amax %.2f: PSNR over the mark %.1f dB < %.0f (fixed %d, opaque %d)", amax, p, want, fixed, opaque)
		}
		if fixed == 0 || (amax >= opaqueAlpha) != (opaque > 0) {
			t.Errorf("amax %.2f: fixed %d, opaque %d", amax, fixed, opaque)
		}
		if s := Score(out, m); s > 0.15 {
			t.Errorf("amax %.2f: score after removal %.3f, a trace is left", amax, s)
		}
	}
}

// Flow images are 4:2:0 JPEGs compressed after stamping, so colour is smeared across the mark's edge. Inverting that
// with the sharp mask on each RGB channel leaves a coloured outline around the old mark (0.6–0.75 on real images,
// stronger than in the marked original). After removal the colour must not follow the mark's outline.
func TestJPEGRemovalLeavesNoColourFringe(t *testing.T) {
	m, _ := MaskFor(1376, 768)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, stampImage(randomImage(1376, 768, 11), m, 1), &jpeg.Options{Quality: 92}); err != nil {
		t.Fatal(err)
	}
	marked, err := jpeg.Decode(&buf)
	if err != nil {
		t.Fatal(err)
	}
	out, _, _ := Remove(marked, m)
	for ch, name := range map[int]string{1: "Cb", 2: "Cr"} {
		if e := edgeCorrelation(out, m, ch); e > 0.2 {
			t.Errorf("%s: colour outline in the mark's shape remains (correlation %.2f > 0.2)", name, e)
		}
	}
}

// On a smooth, nearly white background the mark lifts its core by only ~12 levels yet stays clearly visible. It must
// be removed, not skipped as too faint (it was, when strength was fitted per image and faint marks were left alone).
func TestProcessRemovesMarkOnWhiteBackground(t *testing.T) {
	m, _ := MaskFor(1792, 2400)
	orig := image.NewRGBA(image.Rect(0, 0, 1792, 2400))
	for y := range 2400 {
		for x := range 1792 {
			v := uint8(212 + (x+y)%7) // smooth white fabric, background ~215
			orig.Set(x, y, color.RGBA{v, v, v - 4, 255})
		}
	}
	out, res := Process(stampImage(orig, m, 1), DefaultThreshold)
	if res.Status != Removed || out == nil {
		t.Fatalf("want %v, got %+v", Removed, res)
	}
	if p := psnr(out, orig, m); p < 40 {
		t.Errorf("PSNR over the mark %.1f dB < 40", p)
	}
}

// ProcessFile rewrites a marked file and never touches an unmarked or too small one.
func TestProcessFile(t *testing.T) {
	dir := t.TempDir()
	m, _ := MaskFor(1376, 768)

	orig := woodImage(1376, 768, 85)
	marked := writePNG(t, dir, "marked.png", stampImage(orig, m, 1))
	res, err := ProcessFile(marked, DefaultThreshold)
	if err != nil || res.Status != Removed {
		t.Fatalf("want %v, got %+v %v", Removed, res, err)
	}
	got := readRGBA(t, marked)
	if p := psnr(got, orig, m); p < 40 {
		t.Errorf("file after removal is far from the original (PSNR %.1f dB)", p)
	}

	clean := writePNG(t, dir, "clean.png", orig)
	before, _ := os.ReadFile(clean)
	if res, err := ProcessFile(clean, DefaultThreshold); err != nil || res.Status != NoMark {
		t.Errorf("unmarked file: want %v, got %+v %v", NoMark, res, err)
	}
	if after, _ := os.ReadFile(clean); !bytes.Equal(before, after) {
		t.Error("an unmarked file must not be rewritten")
	}

	small := writePNG(t, dir, "small.png", randomImage(100, 100, 1))
	if res, err := ProcessFile(small, DefaultThreshold); err != nil || res.Status != TooSmall {
		t.Errorf("100x100 file: want %v, got %+v %v", TooSmall, res, err)
	}

	if _, err := ProcessFile(writePNG(t, dir, "x.png", orig)+".missing", DefaultThreshold); err == nil {
		t.Error("a missing file must be an error")
	}
	broken := filepath.Join(dir, "broken.jpg")
	_ = os.WriteFile(broken, []byte("not an image"), 0o644)
	if _, err := ProcessFile(broken, DefaultThreshold); err == nil {
		t.Error("an undecodable file must be an error")
	}
}

// JPEG input is re-encoded as JPEG and the mark is gone from the decoded result.
func TestProcessFileJPEG(t *testing.T) {
	m, _ := MaskFor(1376, 768)
	p := filepath.Join(t.TempDir(), "marked.jpg")
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, stampImage(woodImage(1376, 768, 60), m, 1), &jpeg.Options{Quality: 92}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ProcessFile(p, DefaultThreshold)
	if err != nil || res.Status != Removed {
		t.Fatalf("want %v, got %+v %v", Removed, res, err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, format, err := image.Decode(f)
	if err != nil || format != "jpeg" {
		t.Fatalf("result must stay a JPEG: format %q, %v", format, err)
	}
	if s := Score(img, m); s > 0.15 {
		t.Errorf("score after removal %.3f, a trace is left", s)
	}
}

// Processing an already-cleaned file must leave it alone: inverting a mark that is not there prints a dark ✦. The
// guard holds even when the detection threshold is forced to 0.
func TestSecondPassLeavesCleanImageAlone(t *testing.T) {
	m, _ := MaskFor(1376, 768)
	img := image.NewRGBA(image.Rect(0, 0, 1376, 768))
	for y := range 768 {
		for x := range 1376 {
			v := uint8(40 + (x*7+y*13)%60) // even, dark texture
			img.Set(x, y, color.RGBA{v, v, v, 255})
		}
	}
	p := writePNG(t, t.TempDir(), "twice.png", stampImage(img, m, 1))
	first, err := ProcessFile(p, DefaultThreshold)
	if err != nil || first.Status != Removed || first.Fixed == 0 {
		t.Fatalf("first pass: %+v %v", first, err)
	}
	second, err := ProcessFile(p, DefaultThreshold)
	if err != nil || (second.Status != NoMark && second.Status != NotAMark) {
		t.Fatalf("second pass must leave the file alone: %+v %v", second, err)
	}
	// With the threshold forced to 0 any guard may refuse (both scores are noise); the file must stay as it is.
	forced, err := ProcessFile(p, 0)
	if err != nil || forced.Status == Removed {
		t.Fatalf("second pass at threshold 0 must still leave the file alone: %+v %v", forced, err)
	}
}

// Other Google apps stamp a 96 px star on 2752x1536 images, centred where the Flow mark would be, so the Flow mask
// also scores on it. That star is also stronger (peak alpha ~0.51 against the Flow stamp's 0.325), so subtracting the
// Flow mask only takes out part of it and the already-clean guard does not fire: the image would be rewritten with a
// damaged mark. It must be recognised as another app's mark and left alone.
func TestProcessLeavesOtherAppMarkAlone(t *testing.T) {
	const width, height = 2752, 1536
	var big *Mask
	for _, o := range otherMasks(width, height) {
		if o.star == 96 && o.gap == 64 {
			big = o.mask
		}
	}
	if big == nil {
		t.Fatal("no 96 px placement 64 px from the corner")
	}
	img := stampImage(woodImage(width, height, 110), big, 0.51/0.325)
	out, res := Process(img, DefaultThreshold)
	if res.Status != OtherMark || out != nil {
		t.Errorf("want %v and no output, got %+v", OtherMark, res)
	}
}

// Locate finds the stamp at a position that is not the predicted one, as needed to check a new image size.
func TestLocateFindsStamp(t *testing.T) {
	const width, height, x, y = 1376, 768, 1203, 611
	img := stampImage(woodImage(width, height, 70), stampAt(width, height, x, y), 1)
	found, score := Locate(img)
	if found.X != x || found.Y != y || score < DefaultThreshold {
		t.Errorf("Locate = %d,%d score %.3f, want %d,%d", found.X, found.Y, score, x, y)
	}
}
