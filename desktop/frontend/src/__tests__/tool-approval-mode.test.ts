// Run: tsx src/__tests__/tool-approval-mode.test.ts

import { readFileSync } from "node:fs";
import { normalizeToolApprovalMode } from "../lib/types";
import { en } from "../locales/en";
import { zh } from "../locales/zh";
import { zhTW } from "../locales/zh-TW";

let passed = 0;
let failed = 0;

function eq(a: unknown, b: unknown, label: string) {
  if (a === b) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: expected ${JSON.stringify(b)}, got ${JSON.stringify(a)}\n`);
    failed += 1;
  }
}

console.log("\npermission presets");

eq(normalizeToolApprovalMode("read-only"), "read-only", "read-only remains canonical");
eq(normalizeToolApprovalMode("workspace-write"), "workspace-write", "workspace-write remains canonical");
eq(normalizeToolApprovalMode("danger-full-access"), "danger-full-access", "full access remains canonical");
eq(normalizeToolApprovalMode("ask"), "read-only", "legacy ask migrates to read-only");
eq(normalizeToolApprovalMode("auto"), "workspace-write", "legacy auto migrates to workspace-write");
eq(normalizeToolApprovalMode("yolo"), "workspace-write", "legacy yolo migrates conservatively");
eq(normalizeToolApprovalMode("unknown"), "read-only", "unknown values fail closed");
eq(normalizeToolApprovalMode("", undefined, false, "workspace-write"), "workspace-write", "new-session fallback uses workspace access");

eq(en["composer.permissionReadOnly"], "Read only", "English read-only label");
eq(en["composer.permissionWorkspaceWrite"], "Workspace access", "English workspace label");
eq(en["composer.permissionFullAccess"], "Full access", "English full-access label");
eq(zh["composer.permissionReadOnly"], "仅可查看", "Simplified Chinese read-only label");
eq(zh["composer.permissionWorkspaceWrite"], "工作区内修改", "Simplified Chinese workspace label");
eq(zh["composer.permissionFullAccess"], "完全权限", "Simplified Chinese full-access label");
eq(zh["composer.permissionRecommended"], "推荐", "Simplified Chinese workspace recommendation");
eq(zh["composer.permissionWorkspaceWriteDesc"], "可读写当前工作区；越界操作需要你授权", "Simplified Chinese workspace description");
eq(zhTW["composer.permissionReadOnly"], "僅可查看", "Traditional Chinese read-only label");
eq(zhTW["composer.permissionWorkspaceWrite"], "工作區內修改", "Traditional Chinese workspace label");
eq(zhTW["composer.permissionFullAccess"], "完全權限", "Traditional Chinese full-access label");

const styles = readFileSync(new URL("../styles.css", import.meta.url), "utf8");
eq(styles.includes(".composer-choice--permission-read-only { color: var(--composer-permission-ask); }"), true, "read-only keeps the 1.38.7 Ask blue");
eq(styles.includes(".composer-choice--permission-workspace-write { color: var(--ok); }"), true, "workspace-write keeps the 1.38.7 Auto green");
eq(styles.includes(".composer-choice--permission-danger-full-access { color: var(--err); }"), true, "full access keeps the 1.38.7 Yolo red");

console.log(`\n${passed} passed, ${failed} failed, ${passed + failed} total`);
if (failed > 0) process.exit(1);
