// Run: tsx src/__tests__/inbox-error-localization.test.ts

import { formatInboxCancelError, formatInboxError } from "../lib/inboxError";

let passed = 0;
let failed = 0;

function eq(actual: unknown, expected: unknown, label: string) {
  if (actual === expected) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}: got ${JSON.stringify(actual)}, want ${JSON.stringify(expected)}\n`);
    failed += 1;
  }
}

console.log("\ninbox error localization");

const stableCases = [
  ["inbox_paused", "收件箱已暂停"],
  ["inbox_capacity_items", "收件箱已达到条目上限"],
  ["inbox_capacity_bytes", "收件箱已达到容量上限"],
  ["inbox_item_too_large", "这条指令太大，无法加入收件箱"],
  ["inbox_item_not_found", "这条收件箱指令已不存在"],
  ["inbox_invalid_state", "当前状态下无法操作这条收件箱指令"],
  ["inbox_schema_readonly", "收件箱由较新版本创建，当前只能读取"],
  ["inbox_closed", "收件箱已关闭"],
  ["inbox_empty", "不能加入空白指令"],
  ["inbox_idempotency_conflict", "这条指令与已提交的请求冲突"],
  ["channel_read_only", "当前会话为只读，无法修改收件箱"],
  ["workspace_starting", "工作区还在启动，请稍后重试"],
  ["workspace_start_failed", "工作区启动失败"],
] as const;

for (const [code, expected] of stableCases) {
  eq(formatInboxError(new Error(`reasonix_error:${code}`), "zh"), expected, `zh maps ${code}`);
}

eq(formatInboxError(new Error("inbox is paused"), "zh"), "收件箱已暂停", "legacy backend English is localized");
eq(formatInboxError(new Error("workspace failed to start: internal detail"), "zh"), "工作区启动失败", "legacy startup detail is sanitized and localized");
eq(formatInboxError("reasonix_error:inbox_paused", "zh-TW"), "收件匣已暫停", "traditional Chinese maps stable code");
eq(formatInboxError(new Error("reasonix_error:inbox_paused"), "en"), "Inbox is paused", "English maps stable code");
eq(
  formatInboxCancelError(new Error("reasonix_error:inbox_invalid_state"), "zh"),
  "取消失败：当前状态下无法操作这条收件箱指令",
  "cancel failures localize both context and stable code",
);

const diagnostic = "filesystem detail: /private/example";
eq(formatInboxError(new Error(diagnostic), "zh"), diagnostic, "unknown diagnostic details stay intact");
eq(formatInboxCancelError(new Error(diagnostic), "zh"), `取消失败：${diagnostic}`, "localized cancel context preserves unknown diagnostics");

if (failed > 0) {
  process.stderr.write(`\n${failed} failed, ${passed} passed\n`);
  process.exit(1);
}
process.stdout.write(`\n${passed} passed\n`);
