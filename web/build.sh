#!/bin/sh
# Builds web/remover.wasm and copies the matching wasm_exec.js (it must come from the same Go version).
set -eu
cd "$(dirname "$0")/.."
GOOS=js GOARCH=wasm go build -trimpath -ldflags="-s -w" -o web/remover.wasm ./cmd/wasm
goroot=$(go env GOROOT)
if [ -f "$goroot/lib/wasm/wasm_exec.js" ]; then
	cp "$goroot/lib/wasm/wasm_exec.js" web/
else
	cp "$goroot/misc/wasm/wasm_exec.js" web/ # Go before 1.24
fi
ls -l web/remover.wasm
