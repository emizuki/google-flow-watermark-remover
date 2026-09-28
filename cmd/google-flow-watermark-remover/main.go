// Command google-flow-watermark-remover removes the visible "✦" mark from images generated in Google Flow.
//
//	google-flow-watermark-remover [-threshold 0.2] [-flow] [-out dir] image.jpg ...
//	google-flow-watermark-remover -locate image.jpg ...
//
// Without -out, files are overwritten in place; with -out, each image is copied into that directory first and only
// the copy is changed. -locate searches each image's corner for the mark and prints where it is, without changing
// anything. Exit status: 0 on success, 1 if a file could not be read or written, 2 on bad usage.
package main

import (
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"

	"github.com/emizuki/google-flow-watermark-remover/watermark"
)

func main() {
	threshold := flag.Float64("threshold", watermark.DefaultThreshold, "minimum detection score to treat an image as marked")
	flow := flag.Bool("flow", false, fmt.Sprintf("images are known to come from Flow: use threshold %.1f", watermark.FlowThreshold))
	outDir := flag.String("out", "", "write cleaned copies into this directory instead of overwriting the inputs")
	locate := flag.Bool("locate", false, "report where the mark is in each image and change nothing")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: google-flow-watermark-remover [flags] image...\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(2)
	}
	if *flow {
		*threshold = watermark.FlowThreshold
	}
	if *outDir != "" && !*locate {
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}

	failed := false
	for _, src := range flag.Args() {
		var err error
		if *locate {
			err = locateMark(src)
		} else {
			err = removeMark(src, *outDir, *threshold)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", src, err)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

func removeMark(src, outDir string, threshold float64) error {
	target := src
	if outDir != "" {
		target = filepath.Join(outDir, filepath.Base(src))
		if err := copyFile(src, target); err != nil {
			return err
		}
	}
	res, err := watermark.ProcessFile(target, threshold)
	if err != nil {
		return err
	}
	if res.Status == watermark.Removed {
		fmt.Printf("%s: removed · %dx%d · %d px · score %.2f -> %s\n",
			src, res.Width, res.Height, res.Fixed, res.Score, target)
	} else {
		fmt.Printf("%s: unchanged · %dx%d · %s\n", src, res.Width, res.Height, res.Detail)
	}
	return nil
}

func locateMark(src string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(f)
	_ = f.Close()
	if err != nil {
		return err
	}
	b := img.Bounds()
	found, score := watermark.Locate(img)
	fmt.Printf("%s: %dx%d · mark at %d,%d (margin right %d, bottom %d) · score %.2f", src, b.Dx(), b.Dy(),
		found.X, found.Y, b.Dx()-found.X-found.W, b.Dy()-found.Y-found.H, score)
	if m, ok := watermark.MaskFor(b.Dx(), b.Dy()); ok {
		fmt.Printf(" · predicted %d,%d", m.X, m.Y)
	}
	fmt.Println()
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
