#!/usr/bin/env node

/**
 * Single-scroll-writer contract for the native transcript viewport.
 *
 * Only the files in ALLOWED_WRITERS may issue imperative scroll calls
 * (scrollTop / scrollTo / scrollBy / virtualizer scroll APIs) against the
 * transcript: the generation-aware TranscriptViewportWriter.
 * Every other module must route through TranscriptKernel's
 * dispatch/writeOffset API. This guards the "one writer owns scrollTop"
 * invariant that keeps user scrolls, tail-follow, and anchor recovery from
 * fighting each other (#8657).
 *
 * Other virtualized surfaces (WorkspacePanel, VirtualMenu, LineNumberCode)
 * own independent scrollers and are out of scope.
 */

import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const SOURCE_ROOT = fileURLToPath(new URL("../src", import.meta.url));

// Every kernel/adapter command routes through this one gateway.
const ALLOWED_WRITERS = new Set([
  "lib/transcriptViewportWriter.ts",
]);

// Raw `.scrollTop` writes bypass the kernel entirely. The allowed
// set is deliberate: the Transcript writer is the sole fenced gateway; all
// remaining entries are non-transcript (or natively paired with the arbiter):
// - lib/useReasoningScrollFollow.ts: an inner reasoning pane, not Transcript.
// - components/SettingsPanel.tsx: the settings overlay's own scroller.
// - components/WorkspacePanel.tsx: the project tree's own scroller.
// - components/editors/LineNumberCode.tsx: the file viewer's own scroller —
//   resets scroll when a virtual file is replaced by a non-virtual one.
// - custom/features/heartbeat/HeartbeatPanel.tsx: the heartbeat list's custom
//   scrollbar thumb drag, mapped to its own scroller.
const ALLOWED_RAW_SCROLLTOP = new Set([
  "lib/transcriptViewportWriter.ts",
  "lib/useReasoningScrollFollow.ts",
  "components/SettingsPanel.tsx",
  "components/RemoteConnectWizard.tsx",
  "components/WorkspacePanel.tsx",
  "components/editors/LineNumberCode.tsx",
  "custom/features/heartbeat/HeartbeatPanel.tsx",
]);
const IMPERATIVE_SCROLL_RE = /\.scroll(?:To|By)\s*\(|\.scrollTo(?:Offset|Index)\s*\(/;
const RAW_SCROLLTOP_WRITE_RE = /\.scrollTop\s*=(?!=)/;
const TRANSCRIPT_SURFACE_RE = /(?:^|\/)(?:transcript[^/]*|useTranscript[^/]*|Transcript[^/]*|MarkdownHistory)\.(?:ts|tsx)$/;

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
  const lines = readFileSync(file, "utf8").split("\n");
  lines.forEach((line, index) => {
    if (TRANSCRIPT_SURFACE_RE.test(relative) && IMPERATIVE_SCROLL_RE.test(line) && !ALLOWED_WRITERS.has(relative)) {
      failures += 1;
      console.error(
        `check-single-scroll-writer: ${relative}:${index + 1} issues an imperative Transcript scroll call outside the writer.\n` +
        `  ${line.trim()}\n` +
        "  Route the write through TranscriptKernel and TranscriptViewportWriter.",
      );
    }
    if (RAW_SCROLLTOP_WRITE_RE.test(line) && !ALLOWED_RAW_SCROLLTOP.has(relative)) {
      failures += 1;
      console.error(
        `check-single-scroll-writer: ${relative}:${index + 1} writes scrollTop outside an explicitly non-Transcript surface.\n` +
        `  ${line.trim()}\n` +
        "  Transcript writes must route through transcriptViewportWriter.ts.",
      );
    }
  });
}

if (failures > 0) {
  console.error(`check-single-scroll-writer: ${failures} violation(s) found.`);
  process.exit(1);
}
console.log("check-single-scroll-writer: OK");
