import test from "node:test";
import assert from "node:assert/strict";
import {
  advanceAnalysis,
  appendAudit,
  filterRows,
  makeAuditEvent,
  stageState,
  validateIntake,
  validateShare,
} from "./ip-platform-state.mjs";

test("intake requires a document name and category", () => {
  const result = validateIntake({ name: " ", category: "" });
  assert.equal(result.valid, false);
  assert.equal(result.errors.name, "请输入文档名称");
  assert.equal(result.errors.category, "请选择文档分类");
});

test("intake normalizes a valid submission", () => {
  const result = validateIntake({ name: "  专利组合说明书.pdf ", category: "专利文档" });
  assert.deepEqual(result, {
    valid: true,
    errors: {},
    value: { name: "专利组合说明书.pdf", category: "专利文档" },
  });
});

test("analysis progression completes at one hundred percent", () => {
  const result = advanceAnalysis({ progress: 92, status: "running" }, 16);
  assert.equal(result.progress, 100);
  assert.equal(result.status, "complete");
});

test("pipeline state distinguishes complete, active and pending", () => {
  assert.equal(stageState(72, 62), "complete");
  assert.equal(stageState(72, 82), "active");
  assert.equal(stageState(72, 100), "pending");
});

test("row filtering combines query and category", () => {
  const rows = [
    { id: "A-1", name: "星穹推理引擎", owner: "算法中心", category: "技术白皮书" },
    { id: "A-2", name: "多模态检索专利", owner: "知识产权部", category: "专利文档" },
  ];
  assert.deepEqual(filterRows(rows, "星穹", "技术白皮书"), [rows[0]]);
  assert.deepEqual(filterRows(rows, "产权部", "all"), [rows[1]]);
});

test("share validation requires enterprise email and expiry", () => {
  assert.equal(validateShare({ recipient: "guest", expires: "" }).valid, false);
  assert.equal(validateShare({ recipient: "partner@example.com", expires: "7d" }).valid, true);
});

test("audit events append newest first and retain evidence", () => {
  const event = makeAuditEvent("创建分享", "已授权 partner@example.com", "测试员");
  const events = appendAudit([{ id: "older" }], event);
  assert.equal(events[0].action, "创建分享");
  assert.equal(events[0].actor, "测试员");
  assert.equal(events[1].id, "older");
});
