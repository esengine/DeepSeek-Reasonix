#!/usr/bin/env node
// Discovery-based test runner: every src/__tests__/*.test.ts{,x} runs as its
// own tsx process (suites install their own jsdom/globals and exit non-zero
// on failure), so adding a test file needs no package.json registration.
// Suites owned by a dedicated pnpm script are excluded here, named with their
// owner, and their absence fails the run so a rename cannot silently drop
// coverage. Fail-fast by default; --keep-going runs everything and summarizes.
import { spawnSync } from "node:child_process";
import { readdirSync } from "node:fs";
import { createRequire } from "node:module";
import { join } from "node:path";

// Resolve the local tsx entry directly so the runner works both under pnpm
// scripts and when invoked as plain `node scripts/run-tests.mjs`.
const tsxCli = createRequire(import.meta.url).resolve("tsx/cli");

const TESTS_DIR = "src/__tests__";

const OWNED_ELSEWHERE = new Map(Object.entries({
  "terminal-events.test.ts": "test:terminal",
  "terminal-store.test.ts": "test:terminal",
  "terminal-output.test.ts": "test:terminal",
  "terminal-theme.test.ts": "test:terminal",
  "task-monitor-navigation.test.ts": "test:task-monitor",
  "workspace-refresh-store.test.ts": "test:workspace",
  "workspace-changes-errors.test.tsx": "test:workspace",
  "rich-composer-selection.test.tsx": "pretest",
  "context-center-contract.test.ts": "pretest",
  "provider-model-cache.test.ts": "pretest",
  "format-tokens.test.ts": "pretest",
  "usage-stats-format.test.ts": "test:usage-stats",
  "usage-stats-panel.test.tsx": "test:usage-stats",
  "settings-responsive-layout.test.ts": "test:settings-responsive",
  "transcript-virtualization.test.tsx": "test:transcript",
  "composer-menu-viewport.test.ts": "test:composer-menu-viewport",
  "virtual-menu-identity.test.tsx": "test:composer-menu-viewport",
  "remote-workspace-launch.test.ts": "test:remote",
  "remote-store.test.ts": "test:remote",
  "remote-error-ux.test.tsx": "test:remote",
  "remote-hosts-page.test.tsx": "test:remote",
  "remote-secret-dialog.test.tsx": "test:remote",
  "remote-server-panel.test.tsx": "test:remote (needs the svg stub register)",
  "updater-shared-state.test.tsx": "test:updater",
  "window-state-ordering.test.ts": "test:window-state",
}));

const keepGoing = process.argv.includes("--keep-going");
const files = readdirSync(TESTS_DIR)
  .filter((name) => /\.test\.tsx?$/.test(name))
  .sort();

for (const [name, owner] of OWNED_ELSEWHERE) {
  if (!files.includes(name)) {
    console.error(`run-tests: excluded suite ${name} (owner: ${owner}) no longer exists — update OWNED_ELSEWHERE`);
    process.exit(1);
  }
}

const suites = files.filter((name) => !OWNED_ELSEWHERE.has(name));
console.log(`run-tests: ${suites.length} discovered suites (${OWNED_ELSEWHERE.size} owned by dedicated scripts)`);

const failures = [];
for (const name of suites) {
  const path = join(TESTS_DIR, name);
  console.log(`\n▶ ${path}`);
  // English locale mirrors the Go convention (LANG=en_US.UTF-8 go test):
  // Node's built-in navigator.language follows the machine's ICU locale, and
  // suites assert English UI strings.
  const env = { ...process.env, LANG: "en_US.UTF-8", LC_ALL: "en_US.UTF-8" };
  const result = spawnSync(process.execPath, [tsxCli, path], { stdio: "inherit", env });
  if (result.error) console.error(`run-tests: spawn failed for ${path}: ${result.error.message}`);
  if (result.status !== 0) {
    if (!keepGoing) {
      console.error(`\nrun-tests: FAILED at ${path}`);
      process.exit(result.status ?? 1);
    }
    failures.push(name);
  }
}

if (failures.length > 0) {
  console.error(`\nrun-tests: ${failures.length}/${suites.length} suites failed:`);
  for (const name of failures) console.error(`  FAIL ${name}`);
  process.exit(1);
}
console.log(`\nrun-tests: all ${suites.length} suites passed`);
