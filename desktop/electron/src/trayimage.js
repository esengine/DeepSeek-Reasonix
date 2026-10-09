"use strict";
const fs = require("node:fs");
const path = require("node:path");

const ASSETS = path.join(__dirname, "..", "assets");
const SCALES = [
  [1, ""],
  [1.25, "@1.25x"],
  [1.5, "@1.5x"],
  [1.75, "@1.75x"],
  [2, "@2x"],
  [2.5, "@2.5x"],
  [3, "@3x"],
];

function trayAsset(scaleFactor) {
  const want = Number.isFinite(scaleFactor) && scaleFactor > 0 ? scaleFactor : 1;
  const hit = SCALES.find(([scale]) => scale >= want - 1e-6) ?? SCALES[SCALES.length - 1];
  return { file: path.join(ASSETS, `tray${hit[1]}.png`), pixels: Math.round(16 * hit[0]) };
}

// Windows rebuilds the tray icon from the 1x bitmap, resizing it to the small
// icon metric, so the file is loaded as 1x pixels of exactly that size. Other
// platforms choose among the @Nx files themselves.
function trayImage(nativeImage, scaleFactor) {
  if (process.platform !== "win32") return nativeImage.createFromPath(path.join(ASSETS, "tray.png"));
  return nativeImage.createFromBuffer(fs.readFileSync(trayAsset(scaleFactor).file));
}

module.exports = { trayAsset, trayImage, SCALES };
