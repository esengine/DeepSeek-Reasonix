#!/usr/bin/env node

/**
 * Single-scroll-writer contract for the transcript Virtuoso handle.
 *
 * Only the files in ALLOWED_WRITERS may issue imperative scroll calls
 * (scrollTo / scrollBy / scrollToIndex) against the transcript Virtuoso
 * handle: the arbiter hook plus its extracted controllers (split out for the
 * file-size budget — still one logical writer, driven only by the arbiter).
 * Every other module must route through the scroll coordinator's
 * dispatch/writeOffset API. This guards the "one writer owns scrollTop"
 * invariant that keeps user scrolls, tail-follow, and anchor recovery from
 * fighting each other (#8657).
 *
 * Other virtualized surfaces (WorkspacePanel, VirtualMenu, LineNumberCode)
 * use @tanstack/react-virtual `virtualizer` instances, not the transcript
 * Virtuoso handle, and are out of scope.
 */

import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const SOURCE_ROOT = fileURLToPath(new URL("../src", import.meta.url));

// Only the scroll-arbiter hook and its extracted controllers may write to the
// transcript Virtuoso handle; every other module routes through the
// dispatch/writeOffset/recovery API.
const ALLOWED_WRITERS = new Set([
  "lib/useTranscriptScrollArbiter.ts",
  "lib/transcriptTailSettle.ts",
]);

// Raw `.scrollTop` writes bypass the Virtuoso handle entirely. The allowed
// set is deliberate and non-transcript (or natively paired with the arbiter):
// - lib/nestedScrollHandoff.ts: the trackpad handoff lane; every write is
//   paired with onParentScrollIntent so the arbiter sees the gesture.
// - components/SettingsPanel.tsx: the settings overlay's own scroller.
// - components/WorkspacePanel.tsx: the project tree's own scroller.
// - components/editors/LineNumberCode.tsx: the file viewer's own scroller —
//   resets scroll when a virtual file is replaced by a non-virtual one.
// - custom/features/heartbeat/HeartbeatPanel.tsx: the heartbeat list's custom
//   scrollbar thumb drag, mapped to its own scroller.
const ALLOWED_RAW_SCROLLTOP = new Set([
  "lib/nestedScrollHandoff.ts",
  "components/SettingsPanel.tsx",
  "components/WorkspacePanel.tsx",
  "components/editors/LineNumberCode.tsx",
  "custom/features/heartbeat/HeartbeatPanel.tsx",
]);
// Matches imperative scroll calls on the transcript Virtuoso handle, whether
// reached through `virtuosoRef.current` directly or a local `handle` alias.
const VIRTUOSO_SCROLL_RE = /(?:virtuoso[A-Za-z]*\.current|\bhandle)\??\.\s*scroll(?:To|By|ToIndex)\s*\(/;

function sourceFiles(root) {
  const files = [];
  const visit = (dir) => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const path = join(dir, entry.name);
      if (entry.isDirectory()) {
        if (entry.name !== "__tests__") visit(path);
      } else if (/\.(?:ts|tsx)$/.test(entry.name) && !/\.test\.(?:ts|tsx)$/.test(entry.name)) {
        files.push(path);
      }
    }
  };
  visit(root);
  return files.sort();
}

let failures = 0;
for (const file of sourceFiles(SOURCE_ROOT)) {
  const relative = file.slice(SOURCE_ROOT.length + 1).replaceAll("\\", "/");
  if (ALLOWED_WRITERS.has(relative)) continue;
  const lines = readFileSync(file, "utf8").split("\n");
  lines.forEach((line, index) => {
    if (!VIRTUOSO_SCROLL_RE.test(line)) return;
    failures += 1;
    console.error(
      `check-single-scroll-writer: ${relative}:${index + 1} issues an imperative Virtuoso scroll call outside the allowed writer modules.\n` +
      `  ${line.trim()}\n` +
      "  Route the write through the transcript scroll coordinator instead (see #8657 scroll-arbiter refactor).",
    );
  });
}

if (failures > 0) {
  console.error(`check-single-scroll-writer: ${failures} violation(s) found.`);
  process.exit(1);
}
console.log("check-single-scroll-writer: OK");
