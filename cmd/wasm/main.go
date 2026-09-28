//go:build js && wasm

// Command wasm exposes the remover to JavaScript for the web page in web/. It registers a global
// object:
//
//	flowWatermark.process(bytes: Uint8Array, threshold: number) → {
//	    status, removed, detail, width, height, score, fixed, mime,
//	    mask?: {x, y, w, h},   // where the mark is expected
//	    bytes?: Uint8Array,    // the cleaned image, present when removed
//	}
//	flowWatermark.thresholds → {default, flow}
//
// Build with web/build.sh.
package main

import (
	"bytes"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"syscall/js"

	"github.com/emizuki/google-flow-watermark-remover/watermark"
)

func main() {
	js.Global().Set("flowWatermark", js.ValueOf(map[string]any{
		"process": js.FuncOf(process),
		"thresholds": map[string]any{
			"default": watermark.DefaultThreshold,
			"flow":    watermark.FlowThreshold,
		},
	}))
	select {} // keep the exported function alive
}

func process(_ js.Value, args []js.Value) any {
	if len(args) == 0 {
		return failure("no image data")
	}
	data := make([]byte, args[0].Get("length").Int())
	js.CopyBytesToGo(data, args[0])
	threshold := watermark.DefaultThreshold
	if len(args) > 1 && args[1].Type() == js.TypeNumber {
		threshold = args[1].Float()
	}

	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return failure("cannot decode image: " + err.Error())
	}
	out, res := watermark.Process(img, threshold)
	result := map[string]any{
		"status":  res.Status.String(),
		"removed": res.Status == watermark.Removed,
		"detail":  res.Detail,
		"width":   res.Width,
		"height":  res.Height,
		"score":   res.Score,
		"fixed":   res.Fixed,
		"mime":    "image/" + format,
	}
	if m, ok := watermark.MaskFor(res.Width, res.Height); ok {
		result["mask"] = map[string]any{"x": m.X, "y": m.Y, "w": m.W, "h": m.H}
	}
	if out == nil {
		return js.ValueOf(result)
	}
	var buf bytes.Buffer
	if err := watermark.Encode(&buf, format, out); err != nil {
		result["removed"] = false
		result["detail"] = err.Error()
		return js.ValueOf(result)
	}
	encoded := js.Global().Get("Uint8Array").New(buf.Len())
	js.CopyBytesToJS(encoded, buf.Bytes())
	result["bytes"] = encoded
	return js.ValueOf(result)
}

func failure(detail string) js.Value {
	return js.ValueOf(map[string]any{"status": "error", "removed": false, "detail": detail})
}
