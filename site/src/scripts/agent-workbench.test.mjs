import assert from "node:assert/strict";
import test from "node:test";
import { createStaticAgentTask, isAgentTerminal, retryTaskDraft, sourceButtonLabel, taskViewModel } from "./agent-workbench.mjs";

test("maps persisted task receipts to a deterministic UI step state", () => {
  const task = {
    state: "running",
    plan: { steps: [{ id: "S1", tool: "search_assets" }, { id: "S2", tool: "read_asset" }] },
    events: [{ type: "step.started", stepId: "S1" }, { type: "step.complete", stepId: "S1" }, { type: "step.started", stepId: "S2" }],
  };
  const view = taskViewModel(task);
  assert.deepEqual(view.steps.map((step) => step.status), ["complete", "active"]);
  assert.equal(view.progress, 50);
  assert.equal(view.terminal, false);
});

test("static acceptance task demonstrates grounded delivery without formal mutation", () => {
  const task = createStaticAgentTask("分析目标资产的依赖影响", "impact_analysis");
  assert.equal(task.state, "complete");
  assert.equal(task.plan.steps.length, 3);
  assert.equal(task.result.quality.evidenceCoverage, 1);
  assert.ok(task.result.findings.every((finding) => finding.sourceIds.length));
  assert.match(task.result.excludedActions.join(" "), /未保存|未发布/);
  assert.equal(isAgentTerminal(task.state), true);
});

test("static acceptance boundary rejects coding and publishing requests", () => {
  for (const prompt of ["帮我写代码并运行", "直接发布 Wiki", "删除正式资产"]) {
    const task = createStaticAgentTask(prompt);
    assert.equal(task.state, "blocked", prompt);
    assert.equal(task.events[0].type, "policy.blocked");
    assert.equal(task.plan, undefined);
  }
});

test("retry draft preserves the failed task intent but never submits it", () => {
  const draft = retryTaskDraft({ prompt: "盘点专利资产", templateId: "asset_inventory", state: "failed" });
  assert.deepEqual(draft, { prompt: "盘点专利资产", templateId: "asset_inventory" });
  assert.equal(retryTaskDraft({ prompt: "仍在运行", state: "running" }), null);
});

test("source buttons use business labels instead of exposing internal ids", () => {
  assert.equal(sourceButtonLabel("IP-REAL-A", 0), "查看相关资产 1");
  assert.equal(sourceButtonLabel("EV-ABC", 1), "查看原文依据 2");
  assert.equal(sourceButtonLabel("REL-ABC", 2), "查看关联关系 3");
});
