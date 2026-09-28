// Runs remover.wasm off the UI thread.
//
// The first message is {base}: the absolute URL of the directory holding wasm_exec.js and remover.wasm.
// Relative URLs would break when the worker is served from a blob: URL. After loading, the worker posts
// {ready, thresholds} or {error}; each {id, bytes, threshold} request is answered with {id, result}.
let ready;

function load(base) {
  importScripts(new URL("wasm_exec.js", base).href);
  const go = new Go();
  return fetch(new URL("remover.wasm", base))
    .then((response) => {
      if (!response.ok) throw new Error(`remover.wasm: HTTP ${response.status}`);
      return response.arrayBuffer();
    })
    .then((buffer) => WebAssembly.instantiate(buffer, go.importObject))
    .then(({ instance }) => {
      go.run(instance); // main() registers self.flowWatermark, then blocks
      postMessage({ ready: true, thresholds: { ...self.flowWatermark.thresholds } });
    });
}

onmessage = async (e) => {
  if (e.data.base) {
    ready = Promise.resolve()
      .then(() => load(e.data.base))
      .catch((err) => postMessage({ error: String(err) }));
    return;
  }
  await ready;
  const { id, bytes, threshold } = e.data;
  try {
    const start = performance.now();
    const result = self.flowWatermark.process(bytes, threshold);
    result.ms = Math.round(performance.now() - start);
    postMessage({ id, result }, result.bytes ? [result.bytes.buffer] : []);
  } catch (err) {
    postMessage({ id, result: { status: "error", removed: false, detail: String(err) } });
  }
};
