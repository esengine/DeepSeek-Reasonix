import assert from "node:assert/strict";
import test from "node:test";
import { createDeepSeekClient } from "./deepseek-client.mjs";

const json = (value, status = 200) => new Response(JSON.stringify(value), { status, headers: { "content-type": "application/json" } });

test("uses DeepSeek JSON Output and normalizes structured IP analysis", async () => {
  let request;
  const client = createDeepSeekClient({
    apiKey: "deepseek-secret",
    fetchImpl: async (url, options) => {
      request = { url, options, body: JSON.parse(options.body) };
      return json({
        id: "chat-real",
        model: "deepseek-v4-flash",
        choices: [{ message: { content: JSON.stringify({
          document: { title: "真实报告", category: "技术设计报告", summary: "平台设计摘要", language: "zh-CN" },
          assets: [{ title: "可溯源 IP Wiki", type: "软件架构", summary: "资产说明", confidence: 0.93, source_quotes: [{ quote: "所有结论都需标注明确的溯源引用编号", section: "产品目标" }] }],
          risks: [],
          wiki: { executive_summary: "Wiki 摘要", key_mechanism: "解析后提取", metrics: [], relationships: [] },
        }) } }],
        usage: { prompt_tokens: 120, completion_tokens: 80, total_tokens: 200 },
      });
    },
  });
  const result = await client.analyzeMarkdown({ markdown: "# MinerU 结果", documentName: "report.html" });
  assert.equal(request.url, "https://api.deepseek.com/chat/completions");
  assert.equal(request.body.response_format.type, "json_object");
  assert.match(request.body.messages[0].content, /JSON/);
  assert.equal(result.analysis.assets.length, 1);
  assert.equal(result.analysis.assets[0].source_quotes[0].section, "产品目标");
  assert.equal(result.usage.totalTokens, 200);
});
test("returns a safe error for malformed model JSON", async () => {
  const client = createDeepSeekClient({
    apiKey: "do-not-leak",
    fetchImpl: async () => json({ choices: [{ message: { content: "not-json do-not-leak" } }] }),
  });
  await assert.rejects(
    client.analyzeMarkdown({ markdown: "content", documentName: "report.html" }),
    (error) => /malformed/.test(error.message) && !error.message.includes("do-not-leak"),
  );
});
