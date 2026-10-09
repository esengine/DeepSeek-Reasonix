"use strict";

const PREF_KEY = "rx-hw-accel";

// A launch-only override for a window that will not draw: the environment
// variable or the flag, neither of which is written back to the preferences.
function shouldDisableGpu({ prefs, env, argv }) {
  return accelerationOff(prefs) || env?.REASONIX_DISABLE_GPU === "1" || (argv ?? []).includes("--disable-gpu");
}

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

function graphicsHandler(app, fromWindow, launchedOff) {
  return (event) => (fromWindow(event) ? graphicsReport(app, launchedOff) : null);
}

module.exports = { graphicsHandler, PREF_KEY, accelerationOff, shouldDisableGpu, graphicsReport };
