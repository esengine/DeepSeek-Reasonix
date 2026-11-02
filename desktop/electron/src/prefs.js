"use strict";
const fs = require("node:fs");
const path = require("node:path");
const { PREF_KEY: SHORTCUT_PREF } = require("./globalshortcut");

// The page's own preferences, kept past the origin that stored them. The kernel
// listens on a fresh loopback port each launch and localStorage is keyed by
// origin, so without this every launch opened on a page that remembered
// nothing. Only the page's own keys are kept.
const PREFIX = "rx-";

function prefsFile(userData) {
  return path.join(userData, "window-prefs.json");
}

// How long a page the agent's browser holds may sit undriven before the shell
// puts it to sleep. Off until the person turns it on; "on" waits 15 minutes.
const PAGE_SLEEP_MS = 15 * 60_000;

function pageSleepAfterMs(prefs) {
  return prefs && prefs["rx-page-sleep"] === "on" ? PAGE_SLEEP_MS : Infinity;
}

// Only page string preferences and the shell-owned shortcut survive a disk read.
function pick(value) {
  const out = {};
  if (!value || typeof value !== "object" || Array.isArray(value)) return out;
  for (const [key, v] of Object.entries(value)) {
    if ((key.startsWith(PREFIX) || key === SHORTCUT_PREF) && typeof v === "string") out[key] = v;
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
function registerPrefs(ipcMain, fileOf, fromWindow, onSave = () => {}, onTheme = () => {}) {
  ipcMain.on("prefs:load", (event) => {
    event.returnValue = fromWindow(event) ? pickPagePrefs(loadPrefs(fileOf())) : {};
  });
  ipcMain.on("prefs:save", (event, prefs) => {
    if (fromWindow(event)) {
      try {
        const current = loadPrefs(fileOf());
        const next = pickPagePrefs(prefs);
        if (current[SHORTCUT_PREF] !== undefined) next[SHORTCUT_PREF] = current[SHORTCUT_PREF];
        savePrefs(fileOf(), next);
        onSave();
      } catch {
        // A preference that did not save is one relaunch of lost state, not a
        // reason to fail the window.
      }
    }
    event.returnValue = true;
  });
  ipcMain.on("prefs:theme", (event, paint) => {
    if (!fromWindow(event) || !event.senderFrame || event.senderFrame !== event.sender.mainFrame) return;
    if (!paint || typeof paint !== "object" || Array.isArray(paint)) return;
    const { mode, scheme } = paint;
    if (mode !== "auto" && mode !== "scheduled" && mode !== "light" && mode !== "dark") return;
    if (scheme !== "light" && scheme !== "dark") return;
    if ((mode === "light" || mode === "dark") && mode !== scheme) return;
    onTheme({ mode, scheme });
  });
}

function pickPagePrefs(prefs) {
  const page = pick(prefs);
  delete page[SHORTCUT_PREF];
  return page;
}

module.exports = { PREFIX, PAGE_SLEEP_MS, pageSleepAfterMs, prefsFile, pick, loadPrefs, savePrefs, registerPrefs };
