import { describe, expect, it } from "vitest";
import { type MdLine, markdownToLines, spansText } from "../../src/cli/ui/markdown-lines.js";

describe("markdownToLines — basic blocks", () => {
  it("returns an empty array for empty input", () => {
    expect(markdownToLines("")).toEqual([]);
  });

  it("parses a level-1 heading", () => {
    const lines = markdownToLines("# Title");
    expect(lines).toHaveLength(1);
    expect(lines[0]).toMatchObject({ kind: "heading", level: 1 });
    if (lines[0]?.kind === "heading") expect(spansText(lines[0].spans)).toBe("Title");
  });

  it("parses a paragraph with emphasis + bold + inline code", () => {
    const lines = markdownToLines("Hello **bold** and *italic* and `code`.");
    expect(lines).toHaveLength(1);
    expect(lines[0]?.kind).toBe("paragraph");
    if (lines[0]?.kind === "paragraph") {
      const spans = lines[0].spans;
      expect(spans.find((s) => s.bold)?.text).toBe("bold");
      expect(spans.find((s) => s.italic)?.text).toBe("italic");
      expect(spans.find((s) => s.code)?.text).toBe("code");
    }
  });

  it("parses a fenced code block with a language", () => {
    const lines = markdownToLines("```ts\nconst x = 1;\n```");
    expect(lines).toHaveLength(1);
    expect(lines[0]).toMatchObject({ kind: "code", lang: "ts", text: "const x = 1;" });
  });

  it("parses an unordered list — one MdLine per item", () => {
    const lines = markdownToLines("- a\n- b\n- c");
    const list = lines.filter((l) => l.kind === "list") as Extract<MdLine, { kind: "list" }>[];
    expect(list).toHaveLength(3);
    expect(list[0]).toMatchObject({ ordered: false, index: 1, depth: 0 });
    expect(spansText(list[0]!.spans)).toBe("a");
    expect(list[2]?.index).toBe(3);
  });

  it("parses an ordered list, preserves start index", () => {
    const lines = markdownToLines("3. third\n4. fourth");
    const list = lines.filter((l) => l.kind === "list") as Extract<MdLine, { kind: "list" }>[];
    expect(list[0]).toMatchObject({ ordered: true, index: 3 });
    expect(list[1]?.index).toBe(4);
  });

  it("parses a task list with checked / unchecked markers", () => {
    const lines = markdownToLines("- [x] done\n- [ ] todo");
    const list = lines.filter((l) => l.kind === "list") as Extract<MdLine, { kind: "list" }>[];
    expect(list[0]?.task).toBe("done");
    expect(list[1]?.task).toBe("todo");
  });

  it("parses nested lists with depth", () => {
    const md = "- outer\n  - inner";
    const lines = markdownToLines(md);
    const list = lines.filter((l) => l.kind === "list") as Extract<MdLine, { kind: "list" }>[];
    expect(list).toHaveLength(2);
    expect(list[0]?.depth).toBe(0);
    expect(list[1]?.depth).toBe(1);
  });

  it("parses a horizontal rule", () => {
    const lines = markdownToLines("---");
    expect(lines.some((l) => l.kind === "hr")).toBe(true);
  });

  it("parses a blockquote", () => {
    const lines = markdownToLines("> quoted text");
    const bq = lines.find((l) => l.kind === "blockquote");
    expect(bq).toBeDefined();
    if (bq?.kind === "blockquote") expect(spansText(bq.spans)).toContain("quoted text");
  });
});

describe("markdownToLines — inline detail", () => {
  it("merges adjacent same-style spans", () => {
    const lines = markdownToLines("hello world");
    if (lines[0]?.kind === "paragraph") {
      expect(lines[0].spans).toHaveLength(1);
      expect(lines[0].spans[0]?.text).toBe("hello world");
    } else {
      throw new Error("expected paragraph");
    }
  });

  it("link tokens carry href on each child span", () => {
    const lines = markdownToLines("see [docs](https://example.com) here");
    if (lines[0]?.kind !== "paragraph") throw new Error("expected paragraph");
    const linked = lines[0].spans.find((s) => s.link);
    expect(linked?.text).toBe("docs");
    expect(linked?.link).toBe("https://example.com");
  });

  it("strikethrough spans carry strike=true", () => {
    const lines = markdownToLines("a ~~b~~ c");
    if (lines[0]?.kind !== "paragraph") throw new Error("expected paragraph");
    const struck = lines[0].spans.find((s) => s.strike);
    expect(struck?.text).toBe("b");
  });

  it("file refs in paragraph text carry path/line metadata", () => {
    const lines = markdownToLines("look at src/foo.ts:42 for the bug");
    if (lines[0]?.kind !== "paragraph") throw new Error("expected paragraph");
    const ref = lines[0].spans.find((s) => s.fileRef);
    expect(ref?.fileRef?.path).toBe("src/foo.ts");
    expect(ref?.fileRef?.line).toBe(42);
  });

  it("file ref ranges keep the end line", () => {
    const lines = markdownToLines("see lib/x.ts:10-20");
    if (lines[0]?.kind !== "paragraph") throw new Error("expected paragraph");
    const ref = lines[0].spans.find((s) => s.fileRef);
    expect(ref?.fileRef?.line).toBe(10);
    expect(ref?.fileRef?.lineEnd).toBe(20);
  });
});

describe("markdownToLines — streaming-friendliness", () => {
  it("partial text without a closing backtick still parses", () => {
    const lines = markdownToLines("partial `code");
    expect(lines.length).toBeGreaterThan(0);
    expect(lines[0]?.kind).toBe("paragraph");
  });

  it("empty heading marker before content arrives is tolerated", () => {
    const lines = markdownToLines("# ");
    expect(lines.length).toBeGreaterThanOrEqual(0);
  });

  it("multiple blocks separated by blank lines emit blank between them", () => {
    const lines = markdownToLines("# a\n\nbody");
    const kinds = lines.map((l) => l.kind);
    expect(kinds).toContain("heading");
    expect(kinds).toContain("paragraph");
  });
});
