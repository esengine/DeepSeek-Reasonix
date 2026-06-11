// Run: tsx src/__tests__/memory-settings-docs.test.ts

import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { instructionDocCreateTargets } from "../components/MemoryPanel";
import { en } from "../locales/en";
import { zh } from "../locales/zh";

const testDir = dirname(fileURLToPath(import.meta.url));
const frontendRoot = resolve(testDir, "..");
const memoryPanel = readFileSync(resolve(frontendRoot, "components/MemoryPanel.tsx"), "utf8");
const styles = readFileSync(resolve(frontendRoot, "styles.css"), "utf8");
const zhDict = zh as Record<string, string>;
const enDict = en as Record<string, string>;

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

function includes(source: string, needle: string, label: string) {
  ok(source.includes(needle), label);
}

console.log("\nmemory settings docs");

includes(memoryPanel, 't("memory.addInstructionFile")', "docs tab renders an add-instruction-file action");
includes(memoryPanel, 't("memory.addInstructionFileHint")', "docs tab explains the instruction-file add flow");
includes(memoryPanel, 't("memory.saveInstructionFile")', "docs tab has an explicit save action for new instruction files");
includes(memoryPanel, "submitDoc", "docs tab has a creation handler instead of relying on memory quick-add");
includes(memoryPanel, "docCreateTargets.map", "docs tab offers only filtered instruction-file create targets");
includes(styles, ".settings-page--manager .mem-add--doc", "instruction-file form overrides manager quick-add grid layout");

ok(
  !String(zhDict["memory.noDocs"]).includes("快速添加") &&
    !String(enDict["memory.noDocs"]).toLowerCase().includes("quick-add"),
  "empty instruction-file copy does not point at memory quick-add",
);

ok(zhDict["memory.addInstructionFile"], "Chinese locale includes add instruction-file label");
ok(enDict["memory.addInstructionFile"], "English locale includes add instruction-file label");
ok(zhDict["memory.saveInstructionFile"], "Chinese locale includes save instruction-file label");
ok(enDict["memory.saveInstructionFile"], "English locale includes save instruction-file label");

const scopes = [
  { scope: "user", path: "C:\\Users\\me\\REASONIX.md" },
  { scope: "project", path: "C:\\repo\\AGENTS.md" },
  { scope: "local", path: "C:\\repo\\AGENTS.local.md" },
];

ok(
  instructionDocCreateTargets(scopes, []).map((s) => s.scope).join(",") === "user,project,local",
  "all writable scopes are creation targets when no instruction docs exist",
);

ok(
  instructionDocCreateTargets(scopes, [
    { scope: "user", path: "c:/users/me/reasonix.md", body: "global rule" },
    { scope: "project", path: "C:\\repo\\CLAUDE.md", body: "project rule" },
  ])
    .map((s) => s.scope)
    .join(",") === "local",
  "existing user/project docs are not offered as overwrite-prone create targets",
);

console.log(`\n${passed} passed, ${failed} failed`);
if (failed > 0) process.exit(1);
