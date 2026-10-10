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

// launchedOff is what this launch applied (the saved choice or a launch-only
// override); savedOff is the saved choice alone. compositing is Electron's raw
// gpu_compositing string, which it documents as usable only after
// gpu-info-update and does not enumerate, so it is reported, never interpreted.
function graphicsReport(app, { launchedOff, savedOff }) {
  let compositing = "";
  try {
    compositing = String(app.getGPUFeatureStatus().gpu_compositing ?? "");
  } catch {
    // Not answerable yet; the page shows only what was applied.
  }
  return { launchedOff, savedOff, compositing };
}

function graphicsHandler(app, fromWindow, launch) {
  return (event) => (fromWindow(event) ? graphicsReport(app, launch) : null);
}

module.exports = { graphicsHandler, PREF_KEY, accelerationOff, shouldDisableGpu, graphicsReport };
