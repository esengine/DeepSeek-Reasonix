"use strict";
// Rasterises assets/icon.svg into every icon file the shell and the installers
// read. The SVG is the only source; the .png/.ico/.icns beside it are outputs.
const fs = require("node:fs");
const path = require("node:path");
const { chromium } = require("playwright");

const ASSETS = path.join(__dirname, "..", "assets");
const ICO_SIZES = [16, 20, 24, 32, 40, 48, 64, 256];
// The tray is drawn at 16 logical pixels. Windows asks for exactly 16*scale
// pixels and resizes whatever it is handed, so every Windows scale has its own
// file (src/trayimage.js picks it).
const TRAY_SCALES = [[1, ""], [1.25, "@1.25x"], [1.5, "@1.5x"], [1.75, "@1.75x"], [2, "@2x"], [2.5, "@2.5x"], [3, "@3x"]];
// The app icon's margin is the platform's to keep; a tray slot has none to spare.
const TRAY_VIEWBOX = 'viewBox="103 103 818 818"';
// PNG payloads for every type modern macOS reads; ic11–ic14 are the @2x slots.
const ICNS_TYPES = [
  ["ic04", 16], ["ic05", 32], ["ic07", 128], ["ic08", 256], ["ic09", 512], ["ic10", 1024],
  ["ic11", 32], ["ic12", 64], ["ic13", 256], ["ic14", 512],
];

async function render(page, svg, size) {
  await page.setViewportSize({ width: size, height: size });
  await page.setContent(
    `<html><body style="margin:0;background:transparent">${svg.replace("<svg ", `<svg width="${size}" height="${size}" `)}</body></html>`,
  );
  return page.screenshot({ omitBackground: true, clip: { x: 0, y: 0, width: size, height: size } });
}

function ico(pngs) {
  const head = Buffer.alloc(6 + 16 * pngs.length);
  head.writeUInt16LE(1, 2);
  head.writeUInt16LE(pngs.length, 4);
  let offset = head.length;
  pngs.forEach(([size, data], i) => {
    const at = 6 + 16 * i;
    head[at] = size >= 256 ? 0 : size;
    head[at + 1] = size >= 256 ? 0 : size;
    head.writeUInt16LE(1, at + 4);
    head.writeUInt16LE(32, at + 6);
    head.writeUInt32LE(data.length, at + 8);
    head.writeUInt32LE(offset, at + 12);
    offset += data.length;
  });
  return Buffer.concat([head, ...pngs.map(([, data]) => data)]);
}

function icns(entries) {
  const chunks = entries.map(([type, data]) => {
    const h = Buffer.alloc(8);
    h.write(type, 0, "ascii");
    h.writeUInt32BE(data.length + 8, 4);
    return Buffer.concat([h, data]);
  });
  const body = Buffer.concat(chunks);
  const h = Buffer.alloc(8);
  h.write("icns", 0, "ascii");
  h.writeUInt32BE(body.length + 8, 4);
  return Buffer.concat([h, body]);
}

async function main() {
  const svg = fs.readFileSync(path.join(ASSETS, "icon.svg"), "utf8");
  const browser = await chromium.launch();
  const page = await browser.newPage();
  const cache = new Map();
  const at = async (size) => {
    if (!cache.has(size)) cache.set(size, await render(page, svg, size));
    return cache.get(size);
  };
  try {
    fs.writeFileSync(path.join(ASSETS, "icon.png"), await at(512));
    const traySvg = svg.replace('viewBox="0 0 1024 1024"', TRAY_VIEWBOX);
    for (const [scale, suffix] of TRAY_SCALES) {
      fs.writeFileSync(path.join(ASSETS, `tray${suffix}.png`), await render(page, traySvg, 16 * scale));
    }
    const icoPngs = [];
    for (const size of ICO_SIZES) icoPngs.push([size, await at(size)]);
    fs.writeFileSync(path.join(ASSETS, "icon.ico"), ico(icoPngs));
    const icnsPngs = [];
    for (const [type, size] of ICNS_TYPES) icnsPngs.push([type, await at(size)]);
    fs.writeFileSync(path.join(ASSETS, "icon.icns"), icns(icnsPngs));
  } finally {
    await browser.close();
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
