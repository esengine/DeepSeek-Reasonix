import { describe, expect, it } from "vitest";
import { grammarForPath, parseSource } from "../src/code-query/parser.js";

describe("code-query parser", () => {
  describe("grammarForPath", () => {
    it("maps TS/TSX/JS/JSX extensions to the right grammar", () => {
      expect(grammarForPath("a.ts")).toBe("typescript");
      expect(grammarForPath("a.mts")).toBe("typescript");
      expect(grammarForPath("a.cts")).toBe("typescript");
      expect(grammarForPath("a.tsx")).toBe("tsx");
      expect(grammarForPath("a.js")).toBe("javascript");
      expect(grammarForPath("a.mjs")).toBe("javascript");
      expect(grammarForPath("a.cjs")).toBe("javascript");
      expect(grammarForPath("a.jsx")).toBe("javascript");
    });

    it("returns null for unsupported extensions", () => {
      expect(grammarForPath("a.py")).toBeNull();
      expect(grammarForPath("a.rs")).toBeNull();
      expect(grammarForPath("README.md")).toBeNull();
      expect(grammarForPath("no-extension")).toBeNull();
    });
  });

  describe("parseSource", () => {
    it("parses a TypeScript snippet and yields a non-empty tree", async () => {
      const result = await parseSource(
        "foo.ts",
        "export function add(a: number, b: number): number { return a + b; }",
      );
      expect(result).not.toBeNull();
      expect(result!.grammar).toBe("typescript");
      const root = result!.tree.rootNode;
      expect(root.type).toBe("program");
      expect(root.childCount).toBeGreaterThan(0);
      result!.tree.delete();
    });

    it("parses TSX with JSX literals", async () => {
      const result = await parseSource("foo.tsx", "const x = <div>hi</div>;");
      expect(result).not.toBeNull();
      expect(result!.grammar).toBe("tsx");
      expect(result!.tree.rootNode.hasError).toBe(false);
      result!.tree.delete();
    });

    it("parses JavaScript with JSX", async () => {
      const result = await parseSource("foo.jsx", "const x = <div>hi</div>;");
      expect(result).not.toBeNull();
      expect(result!.grammar).toBe("javascript");
      expect(result!.tree.rootNode.hasError).toBe(false);
      result!.tree.delete();
    });

    it("returns null for unsupported file extensions", async () => {
      expect(await parseSource("x.py", "print('hi')")).toBeNull();
      expect(await parseSource("x.rs", "fn main() {}")).toBeNull();
    });

    it("distinguishes valid syntax from broken syntax", async () => {
      const good = await parseSource("foo.ts", "const x = 1;");
      const bad = await parseSource("foo.ts", "const = ;");
      expect(good!.tree.rootNode.hasError).toBe(false);
      expect(bad!.tree.rootNode.hasError).toBe(true);
      good!.tree.delete();
      bad!.tree.delete();
    });
  });
});
