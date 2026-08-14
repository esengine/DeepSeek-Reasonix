import assert from "node:assert/strict";
import test from "node:test";
import { sampleMarkdownForAnalysis, verifySourceQuote } from "./long-document-sampler.mjs";

function longMarkdown() {
  const sections = Array.from({ length: 18 }, (_, index) => {
    const number = index + 1;
    const title = number === 13 ? "Architecture and key mechanism" : `Section ${number}`;
    return `## ${title}\nanchor-${number} ${String.fromCharCode(97 + (index % 26)).repeat(1_900)} tail-${number}`;
  });
  return `# Real technical report\n${sections.join("\n\n")}\n\n## Conclusion\nfinal-anchor ${"z".repeat(1_900)}`;
}

test("passes short Markdown through without changing source text", () => {
  const markdown = "# Title\n\nExact source quotation.";
  const sampled = sampleMarkdownForAnalysis(markdown, { maxCharacters: 1_000 });
  assert.equal(sampled.markdown, markdown);
  assert.equal(sampled.metadata.strategy, "full");
  assert.equal(sampled.metadata.sourceCharacters, markdown.length);
  assert.equal(sampled.metadata.analysisCharacters, markdown.length);
});

test("samples long Markdown deterministically across the front, middle, priority and tail", () => {
  const markdown = longMarkdown();
  const first = sampleMarkdownForAnalysis(markdown, { maxCharacters: 12_000 });
  const second = sampleMarkdownForAnalysis(markdown, { maxCharacters: 12_000 });
  assert.deepEqual(first, second);
  assert.ok(first.markdown.length <= 12_000);
  assert.equal(first.metadata.strategy, "section-balanced");
  assert.ok(first.metadata.selectedSections < first.metadata.totalSections);
  assert.match(first.markdown, /anchor-1/);
  assert.match(first.markdown, /anchor-9|anchor-10/);
  assert.match(first.markdown, /Architecture and key mechanism/);
  assert.match(first.markdown, /final-anchor/);
  assert.ok(first.metadata.coveragePositions[0] <= 0.1);
  assert.ok(first.metadata.coveragePositions.at(-1) >= 0.9);
});

test("uses most of the analysis budget when a document has many short sections", () => {
  const markdown = Array.from({ length: 64 }, (_, index) => `## Topic ${index + 1}\nanchor-${index + 1} ${"x".repeat(520)}`).join("\n\n");
  const sampled = sampleMarkdownForAnalysis(markdown, { maxCharacters: 20_000 });
  assert.ok(sampled.markdown.length >= 18_000, `only used ${sampled.markdown.length} characters`);
  assert.ok(sampled.markdown.length <= 20_000);
  assert.ok(sampled.metadata.selectedSections > 10);
  assert.match(sampled.markdown, /anchor-64/);
});

test("verifies only normalized continuous substrings from the sampled source", () => {
  const source = "系统采用  Multi-head\nLatent Attention（MLA）降低缓存开销。";
  assert.equal(verifySourceQuote("Multi-head Latent Attention（MLA）降低缓存开销", source), true);
  assert.equal(verifySourceQuote("系统发明了从未出现的全新架构", source), false);
  assert.equal(verifySourceQuote("MLA", source), false, "very short fragments are not reliable evidence");
});
