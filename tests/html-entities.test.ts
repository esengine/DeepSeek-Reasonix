import { describe, expect, it } from "vitest";
import { decodeHtmlEntities } from "../src/cli/ui/html-entities.js";

describe("decodeHtmlEntities (issue #657)", () => {
  it("passes through strings with no '&' fast-path", () => {
    expect(decodeHtmlEntities('{ "apiKey": "value" }')).toBe('{ "apiKey": "value" }');
  });

  it("decodes the common five named entities models leak", () => {
    expect(decodeHtmlEntities("&quot;apiKey&quot;: &quot;sk-...&quot;")).toBe('"apiKey": "sk-..."');
    expect(decodeHtmlEntities("&apos;foo&apos;")).toBe("'foo'");
    expect(decodeHtmlEntities("a &amp; b")).toBe("a & b");
    expect(decodeHtmlEntities("&lt;div&gt;")).toBe("<div>");
  });

  it("decodes &nbsp; to NBSP (terminal renders it as a space cell)", () => {
    expect(decodeHtmlEntities("a&nbsp;b")).toBe("a\u00a0b");
  });

  it('decodes decimal numeric entities (&#34; → ")', () => {
    expect(decodeHtmlEntities("&#34;hello&#34;")).toBe('"hello"');
    expect(decodeHtmlEntities("emoji &#128512;")).toBe("emoji 😀");
  });

  it('decodes hex numeric entities (&#x22; → ", &#x1F600; → 😀)', () => {
    expect(decodeHtmlEntities("&#x22;k&#x22;")).toBe('"k"');
    expect(decodeHtmlEntities("&#x1F600;")).toBe("😀");
  });

  it("leaves unknown named entities alone (don't corrupt prose that quotes entity names)", () => {
    expect(decodeHtmlEntities("the &notreal; entity")).toBe("the &notreal; entity");
  });

  it("is case-insensitive on named entities (&QUOT; works)", () => {
    expect(decodeHtmlEntities("&QUOT;mixed&Quot;")).toBe('"mixed"');
  });

  it("leaves malformed-looking '&' fragments alone", () => {
    expect(decodeHtmlEntities("a & b")).toBe("a & b");
    expect(decodeHtmlEntities("&;")).toBe("&;");
    expect(decodeHtmlEntities("&#;")).toBe("&#;");
  });

  it("decodes the real-world JSON-with-quot pattern from issue #657", () => {
    const input = [
      "{",
      "  &quot;apiKey&quot;: &quot;sk-deepseek&quot;,",
      "  &quot;preset&quot;: &quot;auto&quot;,",
      "  &quot;mcp&quot;: [",
      "  ]",
      "}",
    ].join("\n");
    const expected = [
      "{",
      '  "apiKey": "sk-deepseek",',
      '  "preset": "auto",',
      '  "mcp": [',
      "  ]",
      "}",
    ].join("\n");
    expect(decodeHtmlEntities(input)).toBe(expected);
  });
});
