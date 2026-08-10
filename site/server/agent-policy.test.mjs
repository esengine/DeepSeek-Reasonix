import assert from "node:assert/strict";
import test from "node:test";
import { evaluateAgentRequest, normalizeAgentRequest } from "./agent-policy.mjs";

test("allows bounded document IP and Wiki analysis requests", () => {
  for (const prompt of [
    "盘点与多模态知识抽取相关的 IP 资产，按证据覆盖列出缺口",
    "分析文档版面解析器发生变化后会影响哪些已确认资产",
    "为推理路由方法起草一份 Wiki 更新建议，标明待核实内容",
    "比较知识抽取引擎和版面解析器的定位与依赖关系",
    "核查当前技术方案中的部署风险描述是否有原文依据",
  ]) assert.equal(evaluateAgentRequest(prompt).allowed, true, prompt);
});

test("rejects coding, arbitrary network, destructive, publishing, messaging and administration actions", () => {
  const cases = [
    ["帮我写一个 Python 脚本并运行测试", "coding"],
    ["执行 shell 命令读取服务器文件", "system_access"],
    ["访问 https://example.com 抓取数据", "external_network"],
    ["删除这份 Wiki 并覆盖正式版本", "destructive"],
    ["确认所有关系并直接发布资产", "publication"],
    ["把结果发邮件给客户", "external_message"],
    ["把张三改成管理员并绕过权限", "identity_admin"],
    ["打开数据库执行 SQL 查询", "system_access"],
    ["Write a Python script and run it", "coding"],
    ["Delete all Wiki assets", "destructive"],
    ["Send the report to the customer by email", "external_message"],
    ["Open https://example.com and scrape the page", "external_network"],
  ];
  for (const [prompt, code] of cases) {
    const decision = evaluateAgentRequest(prompt);
    assert.equal(decision.allowed, false, prompt);
    assert.equal(decision.code, code, prompt);
    assert.match(decision.message, /IP|Wiki|文档|边界/);
  }
});

test("does not reject document analysis merely because source topics mention code or deployment", () => {
  assert.equal(evaluateAgentRequest("分析材料中关于代码开源义务的风险与证据").allowed, true);
  assert.equal(evaluateAgentRequest("梳理白皮书描述的部署架构，不执行任何部署操作").allowed, true);
  assert.equal(evaluateAgentRequest("Review the deployment section; do not deploy or run code").allowed, true);
});

test("normalizes task input and caps user-controlled fields", () => {
  const normalized = normalizeAgentRequest({
    prompt: `  ${"盘点资产 ".repeat(1_000)}  `,
    templateId: " impact_analysis ",
    assetIds: ["IP-REAL-ABC", "bad", "IP-REAL-ABC", ...Array.from({ length: 20 }, (_, index) => `IP-X-${index}`)],
  });
  assert.equal(normalized.prompt.length, 4_000);
  assert.equal(normalized.templateId, "impact_analysis");
  assert.deepEqual(normalized.assetIds.slice(0, 2), ["IP-REAL-ABC", "IP-X-0"]);
  assert.equal(normalized.assetIds.length, 10);
  assert.throws(() => normalizeAgentRequest({ prompt: " " }), (error) => error.code === "INVALID_AGENT_REQUEST");
});
