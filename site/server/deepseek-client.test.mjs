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
  const result = await client.analyzeMarkdown({ markdown: "# 产品目标\n\n所有结论都需标注明确的溯源引用编号", documentName: "report.html" });
  assert.equal(request.url, "https://api.deepseek.com/chat/completions");
  assert.equal(request.body.response_format.type, "json_object");
  assert.match(request.body.messages[0].content, /JSON/);
  assert.match(request.body.messages[0].content, /连续子串/);
  assert.equal(result.analysis.assets.length, 1);
  assert.equal(result.analysis.assets[0].source_quotes[0].section, "产品目标");
  assert.equal(result.analysis.assets[0].source_quotes[0].verified, true);
  assert.equal(result.input.strategy, "full");
  assert.deepEqual(result.input.quoteValidation, { total: 1, verified: 1, rejected: 0 });
  assert.equal(result.usage.totalTokens, 200);
});

test("removes hallucinated source quotations and reports section-balanced coverage", async () => {
  const validQuote = "The architecture uses a deterministic routing mechanism for every token.";
  const markdown = Array.from({ length: 20 }, (_, index) => `## Section ${index + 1}\n${index === 0 ? validQuote : `body-${index} ${"x".repeat(3_900)}`}`).join("\n\n");
  const client = createDeepSeekClient({
    apiKey: "deepseek-secret",
    fetchImpl: async () => json({
      id: "chat-quotes",
      model: "deepseek-v4-flash",
      choices: [{ message: { content: JSON.stringify({
        assets: [
          { title: "Routing", source_quotes: [
            { quote: validQuote, section: "Section 1" },
            { quote: "This sentence was invented by the model and is absent.", section: "Section 99" },
          ] },
          { title: "Unsupported invention", source_quotes: [{ quote: "No supporting sentence exists for this asset.", section: "Section 99" }] },
        ],
        wiki: {},
      }) } }],
      usage: {},
    }),
  });
  const result = await client.analyzeMarkdown({ markdown, documentName: "long.pdf" });
  assert.equal(result.input.strategy, "section-balanced");
  assert.ok(result.input.analysisCharacters <= 60_000);
  assert.ok(result.input.selectedSections < result.input.totalSections);
  assert.deepEqual(result.input.quoteValidation, { total: 3, verified: 1, rejected: 2 });
  assert.deepEqual(result.analysis.assets.map((item) => item.title), ["Routing"]);
  assert.deepEqual(result.analysis.assets[0].source_quotes.map((item) => item.quote), [validQuote]);
});

test("canonicalizes relationship endpoints to extracted asset titles", async () => {
  const routingQuote = "Dynamic routing assigns every token to a bounded expert set.";
  const cacheQuote = "Hierarchical cache management reduces repeated memory transfers.";
  const client = createDeepSeekClient({
    apiKey: "deepseek-secret",
    fetchImpl: async () => json({ choices: [{ message: { content: JSON.stringify({
      assets: [
        { title: "Dynamic Routing Architecture", source_quotes: [{ quote: routingQuote, section: "Architecture" }] },
        { title: "Hierarchical Cache Mechanism", source_quotes: [{ quote: cacheQuote, section: "Architecture" }] },
      ],
      wiki: { relationships: [{ source: "Dynamic Routing", relation: "depends_on", target: "Hierarchical Cache" }] },
    }) } }] }),
  });
  const result = await client.analyzeMarkdown({ markdown: `# Architecture\n${routingQuote}\n${cacheQuote}`, documentName: "relations.pdf" });
  assert.deepEqual(result.analysis.wiki.relationships, [{ source: "Dynamic Routing Architecture", relation: "depends_on", target: "Hierarchical Cache Mechanism" }]);
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
