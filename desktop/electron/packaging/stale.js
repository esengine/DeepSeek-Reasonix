"use strict";
const fs = require("node:fs");
const path = require("node:path");

// Exit 0 when the artifact is missing or any file under the given paths is
// newer than it, 1 when it is up to date. run-studio.sh asks this before
// rebuilding; the shell has no portable `find` on every platform it runs on.
const [artifact, ...inputs] = process.argv.slice(2);
if (!artifact) process.exit(2);

let built;
try {
  built = fs.statSync(artifact).mtimeMs;
} catch {
  process.exit(0);
}

const SKIP = new Set(["node_modules", ".git", "dist"]);

function newer(entry) {
  let info;
  try {
    info = fs.statSync(entry);
  } catch {
    return false;
  }
  if (!info.isDirectory()) return info.mtimeMs > built;
  for (const name of fs.readdirSync(entry)) {
    if (SKIP.has(name)) continue;
    if (newer(path.join(entry, name))) return true;
  }
  return false;
}

process.exit(inputs.some(newer) ? 0 : 1);
