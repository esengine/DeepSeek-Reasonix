"use strict";
const fs = require("node:fs");
const path = require("node:path");

// The page's own preferences, kept past the origin that stored them. The kernel
// listens on a fresh loopback port each launch and localStorage is keyed by
// origin, so without this every launch opened on a page that remembered
// nothing. Only the page's own keys are kept.
const PREFIX = "rx-";

function prefsFile(userData) {
  return path.join(userData, "window-prefs.json");
}

// Anything that is not one of the page's own string preferences is dropped, in
// both directions: a hand-edited file or a page bug cannot widen what is kept.
function pick(value) {
  const out = {};
  if (!value || typeof value !== "object" || Array.isArray(value)) return out;
  for (const [key, v] of Object.entries(value)) {
    if (key.startsWith(PREFIX) && typeof v === "string") out[key] = v;
  }
  return out;
}

function loadPrefs(file) {
  try {
    return pick(JSON.parse(fs.readFileSync(file, "utf8")));
  } catch {
    return {};
  }
}

// Written beside and renamed over, so a crash mid-write leaves the last good
// copy rather than half of one.
function savePrefs(file, prefs) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  const tmp = `${file}.tmp`;
  fs.writeFileSync(tmp, JSON.stringify(pick(prefs)));
  fs.renameSync(tmp, file);
}

// Only the Studio window may read or write them; the agent's browser pages run
// in the same process and must not reach this file.
// fileOf is asked on each use: the profile directory is settled during boot,
// after these handlers exist.
function registerPrefs(ipcMain, fileOf, fromWindow, onSave = () => {}) {
  ipcMain.on("prefs:load", (event) => {
    event.returnValue = fromWindow(event) ? loadPrefs(fileOf()) : {};
  });
  ipcMain.on("prefs:save", (event, prefs) => {
    if (fromWindow(event)) {
      try {
        savePrefs(fileOf(), prefs);
        onSave();
      } catch {
        // A preference that did not save is one relaunch of lost state, not a
        // reason to fail the window.
      }
    }
    event.returnValue = true;
  });
}

module.exports = { PREFIX, prefsFile, pick, loadPrefs, savePrefs, registerPrefs };
