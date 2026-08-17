import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const scriptsDir = path.dirname(fileURLToPath(import.meta.url));
const guidePath = path.resolve(scriptsDir, "../../../docs/INTELIFAR-USER-GUIDE.zh-CN.md");
const guide = await readFile(guidePath, "utf8");

test("user guide is written for external small-business users", () => {
  assert.match(guide, /^# intelifar /);
  assert.doesNotMatch(guide, /Inteli[F]ar|Obsidian/);
  for (const reader of ["企业负责人或管理员", "内容维护者", "阅读成员", "外部收件人"]) assert.match(guide, new RegExp(reader));
  for (const task of ["第一次登录", "把一份文档变成知识页面", "检查分析结果并发布", "查找和核对知识", "IP 任务助手", "修改已有 Wiki", "把脱敏内容发给客户", "邀请同事", "每周管理员检查", "遇到问题怎么办"]) assert.match(guide, new RegExp(task));
});

test("user guide keeps the working path clear and avoids engineering jargon", () => {
  assert.match(guide, /选择文件[\s\S]+等待分析[\s\S]+人工复核[\s\S]+发布[\s\S]+安全分享/);
  assert.match(guide, /哪些数据可以直接用于工作/);
  assert.match(guide, /示例数据/);
  assert.doesNotMatch(guide, /\bRBAC\b|\bSQLite\b|\bE2E\b|\bSIEM\b|\bCSP\b|\bscrypt\b|HttpOnly|Obsidian/);
});

test("user guide places enough real screenshots next to user tasks", () => {
  const screenshotReferences = [...guide.matchAll(/!\[[^\]]+\]\(\.\.\/artifacts\/[^)]+\.png\)/g)];
  assert.ok(screenshotReferences.length >= 10, `expected at least 10 screenshots, found ${screenshotReferences.length}`);
  for (const imagePath of [
    "artifacts/smb-p0-review/01-smb-secure-login.png",
    "artifacts/screenshots/02-document-intake.png",
    "artifacts/internet-corpus/02-real-analysis-coverage.png",
    "artifacts/ip-asset-graph/06-neural-panorama.png",
    "artifacts/ip-asset-graph/07-neural-node-inspector.png",
    "artifacts/internet-corpus/03-real-relationship-review.png",
    "artifacts/internet-corpus/04-real-relationship-evidence.png",
    "artifacts/ip-agent/01-agent-workbench.png",
    "artifacts/ip-agent/02-grounded-delivery.png",
    "artifacts/ip-agent/04-boundary-block.png",
    "artifacts/enterprise-95-review/06-real-wiki-final.png",
    "artifacts/member-permissions-module-2026-08-11/05-member-overview-desktop.png",
    "artifacts/member-permissions-module-2026-08-11/02-invitation-dialog-mobile.png",
    "artifacts/smb-p0d-review/05-double-credential-share.png",
    "artifacts/smb-p0d-review/07-public-redacted-wiki.png",
    "artifacts/user-guide-review/04-admin-backup-check.png",
  ]) assert.match(guide, new RegExp(imagePath.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));
});
