"use strict";

const $ = (selector, root = document) => root.querySelector(selector);
const statusText = $("#status");
const results = $("#results");

// The remover runs in a worker so large images do not freeze the page.
const worker = new Worker(new URL("worker.js", location.href));
worker.postMessage({ base: new URL(".", location.href).href });
worker.onerror = (e) => {
  statusText.textContent = "Worker error: " + (e.message || "unknown");
};

const pending = new Map();
let nextId = 0;
let thresholds = { default: 0.2, flow: 0.1 };

worker.onmessage = (e) => {
  const msg = e.data;
  if (msg.error) {
    statusText.textContent = "Could not load the remover: " + msg.error;
    return;
  }
  if (msg.ready) {
    thresholds = msg.thresholds;
    statusText.textContent = "Ready.";
    $("#threshold-default").textContent = thresholds.default;
    $("#threshold-flow").textContent = thresholds.flow;
    return;
  }
  pending.get(msg.id)?.(msg.result);
  pending.delete(msg.id);
};

function processInWorker(bytes, threshold) {
  const id = nextId++;
  return new Promise((resolve) => {
    pending.set(id, resolve);
    worker.postMessage({ id, bytes, threshold }, [bytes.buffer]);
  });
}

function loadImage(url) {
  const img = new Image();
  img.src = url;
  return img.decode().then(() => img);
}

// zoomRect is the square around the mark (the mark box plus one box width on each side), clamped to the image.
function zoomRect(result, width, height) {
  const m = result.mask;
  const side = m ? m.w * 3 : 174;
  const cx = m ? m.x + m.w / 2 : width - side / 2;
  const cy = m ? m.y + m.h / 2 : height - side / 2;
  const x = Math.max(0, Math.min(width - side, Math.round(cx - side / 2)));
  const y = Math.max(0, Math.min(height - side, Math.round(cy - side / 2)));
  return { x, y, side };
}

function zoomCanvas(img, rect, caption) {
  const figure = document.createElement("figure");
  const canvas = document.createElement("canvas");
  canvas.width = canvas.height = rect.side;
  canvas.getContext("2d").drawImage(img, rect.x, rect.y, rect.side, rect.side, 0, 0, rect.side, rect.side);
  const label = document.createElement("figcaption");
  label.textContent = caption;
  figure.append(canvas, label);
  return figure;
}

// compareSlider overlays the cleaned image on the original; dragging moves the split.
function compareSlider(beforeUrl, afterUrl) {
  const box = document.createElement("div");
  box.className = "compare";
  box.innerHTML =
    '<img alt="before"><img class="after" alt="after"><div class="bar"></div>' +
    '<span class="label left">Before</span><span class="label right">After</span>';
  const [before, after] = box.querySelectorAll("img");
  before.src = beforeUrl;
  after.src = afterUrl;
  const bar = $(".bar", box);
  const moveTo = (clientX) => {
    const r = box.getBoundingClientRect();
    const percent = Math.max(0, Math.min(100, ((clientX - r.left) / r.width) * 100));
    after.style.clipPath = `inset(0 0 0 ${percent}%)`;
    bar.style.left = percent + "%";
  };
  box.addEventListener("pointerdown", (e) => {
    box.setPointerCapture(e.pointerId);
    moveTo(e.clientX);
  });
  box.addEventListener("pointermove", (e) => {
    if (box.hasPointerCapture(e.pointerId)) moveTo(e.clientX);
  });
  return box;
}

function statsLine(result) {
  if (result.status === "error") return result.detail;
  const parts = [`${result.width}x${result.height}`, `score ${result.score.toFixed(2)}`];
  if (result.removed) parts.push(`${result.fixed} px changed`);
  if (result.ms != null) parts.push(`${result.ms} ms`);
  return parts.join(" · ") + (result.detail ? " — " + result.detail : "");
}

async function processFile(file) {
  const card = document.createElement("div");
  card.className = "card";
  card.innerHTML = '<div class="head"><span class="name"></span><span class="badge">processing…</span></div><div class="stats"></div>';
  $(".name", card).textContent = file.name;
  results.prepend(card);

  const choice = document.querySelector('input[name="threshold"]:checked').value;
  const result = await processInWorker(new Uint8Array(await file.arrayBuffer()), thresholds[choice]);

  const badge = $(".badge", card);
  badge.textContent = result.removed ? "removed" : "unchanged";
  badge.classList.add(result.removed ? "removed" : "unchanged");
  $(".stats", card).textContent = statsLine(result);
  if (result.status === "error") return;

  const beforeUrl = URL.createObjectURL(file);
  const before = await loadImage(beforeUrl);
  const rect = zoomRect(result, before.naturalWidth, before.naturalHeight);
  const zoom = document.createElement("div");
  zoom.className = "zoom";

  if (!result.removed) {
    const preview = document.createElement("img");
    preview.className = "preview";
    preview.src = beforeUrl;
    zoom.append(zoomCanvas(before, rect, "Mark area (unchanged)"));
    card.append(preview, zoom);
    return;
  }
  const afterUrl = URL.createObjectURL(new Blob([result.bytes], { type: result.mime }));
  const after = await loadImage(afterUrl);
  zoom.append(zoomCanvas(before, rect, "Before (mark area)"), zoomCanvas(after, rect, "After (mark area)"));
  const download = document.createElement("a");
  download.className = "download";
  download.href = afterUrl;
  download.download = file.name.replace(/(\.[^.]+)?$/, ".clean$1");
  download.textContent = "Download cleaned image";
  card.append(compareSlider(beforeUrl, afterUrl), zoom, download);
}

async function handleFiles(files) {
  for (const file of files) {
    if (/^image\/(jpeg|png)$/.test(file.type)) await processFile(file);
  }
}

const drop = $("#drop");
$("#file").addEventListener("change", (e) => {
  handleFiles([...e.target.files]);
  e.target.value = "";
});
drop.addEventListener("dragover", (e) => {
  e.preventDefault();
  drop.classList.add("over");
});
drop.addEventListener("dragleave", () => drop.classList.remove("over"));
drop.addEventListener("drop", (e) => {
  e.preventDefault();
  drop.classList.remove("over");
  handleFiles([...e.dataTransfer.files]);
});
