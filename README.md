# google-flow-watermark-remover

Remove the visible ✦ mark from images generated in Google Flow (Nano Banana, Nano Banana Pro).

**👉 Use it now: [google-flow-watermark-remover.emizuki.com](https://google-flow-watermark-remover.emizuki.com/)**

Drop your Flow images on the page, compare before and after with the slider and the zoomed mark area, then download
the cleaned image. Everything runs in your browser: images are never uploaded.

It reverses the exact alpha blend Google uses to stamp the mark, so the original pixels come back: nothing is painted
over, cropped or invented. Pure Go, standard library only, also available as a command-line tool and a Go package.

## Good to know

- Works best on images **downloaded straight from Flow**, at the size Flow produced them. Resized, cropped or
  re-saved copies move and shrink the mark, so it may not be found, or texture may be mistaken for it.
- Removes the **visible** mark only. SynthID, the invisible watermark Google embeds in the pixels, is not affected.
- JPEG compression that Google applied *after* stamping cannot be undone: at high zoom the mark area can look slightly
  grainier than its surroundings.
- Handles Google Flow's mark only. Images from Google's other apps carry a differently sized mark at a different
  distance from the corner; they are recognised and left unchanged.
- Leaves an image alone when no mark is detected or when it has already been cleaned. Running it twice never prints a
  dark mark.

## Command line

```sh
go install github.com/emizuki/google-flow-watermark-remover/cmd/google-flow-watermark-remover@latest

google-flow-watermark-remover -out clean/ a.jpg b.png   # write cleaned copies to clean/, originals untouched
google-flow-watermark-remover a.jpg                     # overwrite a.jpg in place
google-flow-watermark-remover -flow -out clean/ *.jpg   # images known to come from Flow: lower detection threshold (0.1)
google-flow-watermark-remover -locate a.jpg             # report where the mark is; changes nothing
```

Each file prints one line:

```
a.jpg: removed · 2752x1536 · 905 px · score 0.77 -> clean/a.jpg
clean/a.jpg: unchanged · 2752x1536 · no mark in the corner (score 0.000 < 0.20)
```

Exit status: 0 on success, 1 if a file could not be read or written, 2 on bad usage.

## Go package

```go
import "github.com/emizuki/google-flow-watermark-remover/watermark"

res, err := watermark.ProcessFile("a.jpg", watermark.DefaultThreshold) // overwrites a.jpg if the mark was removed
fmt.Println(res.Status, res.Detail)

clean, info := watermark.Process(img, watermark.DefaultThreshold) // *image.RGBA, or nil when left unchanged
```

`Status` tells what happened: `Removed`, `NoMark`, `OtherMark` (another app's mark), `NotAMark` (already clean) or
`TooSmall`. Lower-level pieces are exported too: `MaskFor`, `Score`, `Remove`, `Locate` and `Encode`.

## How it works

**The mark.** Google blends a white stamp with a fixed alpha mask: `observed = original·(1 − a) + 255·a`. The stamp is
a 58x58 alpha map with a flat core at a ≈ 0.33 (`watermark/sparkle.png`), measured by averaging five dark Flow images,
one per aspect ratio. Knowing `a` per pixel gives the original back: `original = (observed − 255·a) / (1 − a)`.

**Where it is.** The stamp is the same at every image size. Its box sits the same distance from the right and the
bottom edge, and that distance grows with the shorter side of the image:

| Image | Shorter side | Distance measured | 52 + shorter/48 |
|---|---|---|---|
| 1376x768, 768x1376 | 768 | 68 | 68.0 |
| 1200x896, 896x1200 | 896 | 71 | 70.7 |
| 1024x1024 | 1024 | 73 | 73.3 |
| 2752x1536 | 1536 | 84 | 84.0 |
| 1792x2400 | 1792 | 89 | 89.3 |

The formula reproduces every measured placement, so any size is handled without a per-size table. For a size that has
not been measured it is a prediction: if it is wrong the mark does not line up, detection fails, and the image is left
unchanged. `google-flow-watermark-remover -locate` shows where the mark actually is. Searching the whole corner is not
used to place the mask automatically, because on unmarked photos the best texture match reached a score of 0.63,
higher than some real marks.

**Detection.** The score is the correlation between the brightness above the local background and the mask shape.
A real mark reproduces the shape on light or dark, flat or textured backgrounds; on real images it scored 0.20–0.98 at
its position while unmarked positions reached 0.42. The default threshold is 0.2, or 0.1 for images known to come from
Flow.

**Strength.** The mask is subtracted at full strength. On every real image with a known answer the mark matched the
mask: 1.008 and 0.952 on a 2K/1K pair of the same picture (each version shows the clean background under the other's
mark), and about 1.0 on a mark over white fabric. Estimating the strength per image from the surrounding texture was
tried and did worse (0.51 on that fabric image, where a sharp background edge crosses the mark).

**Edges.** Google compresses with 4:2:0 JPEG after stamping. That leaves ringing along the mark's sharp edge, and
smears its colour across the edge because colour is stored at half resolution. After inversion, the 2–4 px edge band
gets a 3x3 median for the ringing, and its colour (Cb/Cr only) is re-solved by harmonic interpolation between the two
sides. Luma is not smoothed, so the wood grain or fabric texture under the mark is kept.

**Other apps.** Google's other image apps stamp the same star at 48 px (32 or 96 px from the corner) or 96 px (64 or
192 px from the corner). On 2752x1536 images the 96 px star is centred exactly where the Flow mark would be, and the
48 px one can sit where texture happens to resemble the Flow mark, so the Flow mask alone can score on them. Before
removing, the same star is scored at those placements too (built from the Flow stamp; on real marks it scores up to
1.00), and the image is left unchanged if any of them matches better than the Flow mark. On 29 public sample images
from those apps, 4 were damaged without this check; all 29 are now left unchanged.

**Guards.** Nothing is changed when the image is too small, the score is below the threshold, another app's mark
matches better, or the result turns darker than its surroundings in the mark's shape (an already-clean image).

## Tests

```sh
go test ./...
```

The tests use synthetic images and the real stamp. They check that inversion restores the original (PSNR ≥ 40 dB on
translucent marks), that a mark on a nearly white background is removed, that no colour outline survives a JPEG round
trip, that the position formula matches every measured placement, that another app's mark is left alone, that
unmarked and too small images are left alone, and that a second pass never prints a dark mark.

## License

[MIT](LICENSE)
