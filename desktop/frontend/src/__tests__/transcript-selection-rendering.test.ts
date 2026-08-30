// Run: tsx src/__tests__/transcript-selection-rendering.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const testDir = dirname(fileURLToPath(import.meta.url));
const styles = readFileSync(resolve(testDir, "../components/TranscriptSelectionMenu.css"), "utf8");
const transcriptSelectionSource = readFileSync(
  resolve(testDir, "../components/TranscriptSelectionMenu.tsx"),
  "utf8",
);
const terminalSource = readFileSync(resolve(testDir, "../components/TerminalPanel.tsx"), "utf8");

let passed = 0;
let failed = 0;

function ok(value: unknown, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

function ruleBody(selector: string): string {
  const start = styles.indexOf(`${selector} {`);
  if (start < 0) return "";
  const bodyStart = styles.indexOf("{", start) + 1;
  const bodyEnd = styles.indexOf("\n}", bodyStart);
  return bodyEnd < 0 ? "" : styles.slice(bodyStart, bodyEnd);
}

console.log("\ntranscript selection rendering");

ok(
  transcriptSelectionSource.includes('data-surface="transcript"')
    && transcriptSelectionSource.includes("data-state={actionOverlay.phase}")
    && transcriptSelectionSource.includes("aria-hidden={actionOverlay.phase !== \"open\"}")
    && transcriptSelectionSource.includes("disabled={actionOverlay.phase !== \"open\"}")
    && transcriptSelectionSource.includes('tabIndex={actionOverlay.phase === "open" ? 0 : -1}'),
  "transcript selection action keeps one state-driven, accessibility-safe portal host",
);

ok(
  transcriptSelectionSource.includes("actionOverlayStateRef.current.action")
    && !transcriptSelectionSource.includes("{action && typeof document !== \"undefined\" && createPortal("),
  "selection state changes neither remount the portal nor reinstall listeners through an action dependency",
);

const closedRule = ruleBody('.transcript-selection-action[data-surface="transcript"]');
const openRule = ruleBody(
  '.transcript-selection-action[data-surface="transcript"][data-state="open"]',
);
const windowsRule = ruleBody(
  ':root[data-platform="windows"] .transcript-selection-action[data-surface="transcript"]',
);

ok(
  /visibility:\s*hidden;/.test(closedRule)
    && /opacity:\s*0;/.test(closedRule)
    && /pointer-events:\s*none;/.test(closedRule)
    && /animation:\s*none;/.test(closedRule),
  "closed and positioning transcript actions stay mounted without painting or hit testing",
);

ok(
  /visibility:\s*visible;/.test(openRule)
    && /opacity:\s*1;/.test(openRule)
    && /pointer-events:\s*auto;/.test(openRule)
    && /animation:\s*menu-pop-in/.test(openRule),
  "non-Windows transcript actions retain the existing open animation",
);

ok(
  /background-color:\s*Canvas;/.test(windowsRule)
    && /background-image:\s*linear-gradient\(var\(--bg-elev\),\s*var\(--bg-elev\)\);/.test(windowsRule)
    && /-webkit-backdrop-filter:\s*none;/.test(windowsRule)
    && /backdrop-filter:\s*none;/.test(windowsRule)
    && /animation:\s*none;/.test(windowsRule)
    && /transition:\s*none;/.test(windowsRule)
    && /transform:\s*none;/.test(windowsRule)
    && /isolation:\s*isolate;/.test(windowsRule),
  "Windows transcript actions use an opaque non-transforming WebView2 paint path",
);

ok(
  terminalSource.includes('className="transcript-selection-action"')
    && !terminalSource.includes('data-surface="transcript"'),
  "the Windows transcript paint override does not affect terminal selection actions",
);

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
