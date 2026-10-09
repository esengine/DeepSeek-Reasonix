"use strict";

const PREF_KEY = "rx-hw-accel";

// Chromium reads this before the app is ready, so a saved "off" only reaches
// the launch after the one that saved it.
function accelerationOff(prefs) {
  return prefs?.[PREF_KEY] === "off";
}

// launchedOff is what this launch applied; compositing is what Chromium ended
// up with, which differs when a driver is refused without being asked.
function graphicsReport(app, launchedOff) {
  let compositing = "";
  try {
    compositing = String(app.getGPUFeatureStatus().gpu_compositing ?? "");
  } catch {
    // Not answerable yet; the page shows only what was applied.
  }
  return { launchedOff, compositing };
}

module.exports = { PREF_KEY, accelerationOff, graphicsReport };
