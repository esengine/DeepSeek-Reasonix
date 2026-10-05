"use strict";
const fs = require("node:fs");
const path = require("node:path");
const { execFileSync } = require("node:child_process");

// The Wails shell installed as ReasonixStudio.app and this one installs as
// "Reasonix Studio.app", so a person who downloaded 2.11 while 2.10 was on disk
// has both: two icons over one data home, where the single-instance lock means
// clicking either raises the same window. A self-update does not produce this —
// it renames the replacement onto the path it replaced.
//
// Only an install left behind is offered. Nothing is removed without an answer,
// and the answer is remembered: a refusal asked again every launch is the
// annoyance this is meant to end.
const BUNDLE_ID = "io.reasonix.studio";
// What we published under. A path is a historical fact about our own releases
// rather than a guess, which is why this one is written down; the search above
// it is what finds an install that moved.
const PUBLISHED_PATH = "/Applications/ReasonixStudio.app";
const RECEIPT = "legacy-bundle.json";

// found reports the bundles carrying our identifier, minus the one running.
// Spotlight is asked by identifier rather than by name because a bundle a
// person renamed is still the same application, and a name match would miss it.
function found(ownBundle) {
  const seen = new Set();
  try {
    const out = execFileSync("mdfind", [`kMDItemCFBundleIdentifier == '${BUNDLE_ID}'`], {
      encoding: "utf8",
      timeout: 10000,
    });
    for (const line of out.split("\n")) {
      const p = line.trim();
      if (p.endsWith(".app")) seen.add(p);
    }
  } catch {
    // Spotlight off or refusing: the published path is the fallback, not a
    // reason to skip the check.
  }
  if (fs.existsSync(PUBLISHED_PATH)) seen.add(PUBLISHED_PATH);
  seen.delete(ownBundle);
  return [...seen].filter((p) => p !== ownBundle && fs.existsSync(p));
}

// ownBundle is the .app this process runs inside, resolved from the executable
// rather than assumed: the answer decides which bundle is *not* offered for
// removal, so reading it wrong removes the running application.
function ownBundle(exe) {
  const marker = `.app${path.sep}Contents${path.sep}MacOS${path.sep}`;
  const at = exe.indexOf(marker);
  return at < 0 ? "" : exe.slice(0, at + ".app".length);
}

function receiptPath(userData) {
  return path.join(userData, RECEIPT);
}

function answered(userData, bundle) {
  try {
    const seen = JSON.parse(fs.readFileSync(receiptPath(userData), "utf8"));
    return Array.isArray(seen.declined) && seen.declined.includes(bundle);
  } catch {
    return false;
  }
}

function remember(userData, bundle) {
  let declined = [];
  try {
    const seen = JSON.parse(fs.readFileSync(receiptPath(userData), "utf8"));
    if (Array.isArray(seen.declined)) declined = seen.declined;
  } catch {
    // No receipt yet, or one this build cannot read: either way the answer
    // being recorded now is the whole of what the next launch needs.
  }
  if (!declined.includes(bundle)) declined.push(bundle);
  try {
    fs.writeFileSync(receiptPath(userData), JSON.stringify({ declined }, null, 2));
  } catch {
    // A receipt that cannot be written costs one repeated question, which is
    // not worth failing a launch over.
  }
}

// offerCleanup asks about each leftover install and trashes the ones allowed.
// Trash rather than delete, because this removes an application somebody else
// may still want and only one of those two is reversible.
async function offerCleanup({ packaged, execPath, userData, ask, trash, find = found }) {
  if (process.platform !== "darwin" || !packaged) return [];
  const own = ownBundle(execPath);
  if (!own) return [];
  const removed = [];
  // The search is a parameter so the decision can be tested against a known
  // set: driven through Spotlight it is whatever that machine happens to hold.
  for (const bundle of find(own)) {
    if (bundle === own) continue;
    if (answered(userData, bundle)) continue;
    if (!(await ask(bundle))) {
      remember(userData, bundle);
      continue;
    }
    try {
      await trash(bundle);
      removed.push(bundle);
    } catch (err) {
      // A bundle that will not move is one the person can drag out themselves;
      // asking again next launch would not make it movable.
      console.error("reasonix-studio: could not trash", bundle, err.message);
      remember(userData, bundle);
    }
  }
  return removed;
}

module.exports = { offerCleanup, ownBundle };
