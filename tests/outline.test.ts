import { describe, expect, it } from "vitest";
import { extractOutline, formatOutline } from "../src/tools/fs/outline.js";

function lines(src: string): string[] {
  return src.split(/\r?\n/);
}

describe("extractOutline", () => {
  describe("TypeScript / JavaScript", () => {
    it("captures top-level exports with kind + name", () => {
      const src = lines(
        [
          "export function alpha() {}",
          "export class Beta {}",
          "export const gamma = 1;",
          "export interface Delta {}",
          "export type Epsilon = string;",
          "export enum Zeta { A }",
        ].join("\n"),
      );
      const out = extractOutline("foo.ts", src);
      expect(out.map((e) => e.text)).toEqual([
        "export function alpha",
        "export class Beta",
        "export const gamma",
        "export interface Delta",
        "export type Epsilon",
        "export enum Zeta",
      ]);
    });

    it("ignores indented (non-top-level) exports", () => {
      const src = lines(
        [
          "class Outer {",
          "  export function inner() {} // syntactically invalid but worth covering",
          "}",
        ].join("\n"),
      );
      expect(extractOutline("foo.ts", src)).toEqual([]);
    });

    it("handles tsx/jsx/mjs/cjs/mts/cts extensions", () => {
      const src = lines("export const x = 1;");
      for (const name of ["a.tsx", "a.jsx", "a.mjs", "a.cjs", "a.mts", "a.cts", "a.js"]) {
        const out = extractOutline(name, src);
        expect(out, name).toHaveLength(1);
        expect(out[0]!.text).toBe("export const x");
      }
    });
  });

  describe("Python", () => {
    it("captures top-level def and class", () => {
      const src = lines(
        [
          "def foo():",
          "    pass",
          "",
          "class Bar:",
          "    def method(self):",
          "        pass",
          "",
          "async def baz():",
          "    pass",
        ].join("\n"),
      );
      const out = extractOutline("a.py", src);
      expect(out).toEqual([
        { line: 1, text: "def foo" },
        { line: 4, text: "class Bar" },
        { line: 8, text: "def baz" },
      ]);
    });

    it("skips indented methods inside a class", () => {
      const src = lines("class Foo:\n    def method(self):\n        pass");
      const out = extractOutline("a.py", src);
      expect(out).toEqual([{ line: 1, text: "class Foo" }]);
    });
  });

  describe("Go", () => {
    it("captures top-level func/type/var/const", () => {
      const src = lines(
        [
          "package main",
          "",
          "func Foo() {}",
          "type Bar struct {}",
          "var Baz = 1",
          "const Qux = 2",
          "",
          "func (r *Bar) Method() {}",
        ].join("\n"),
      );
      const out = extractOutline("a.go", src);
      expect(out.map((e) => e.text)).toEqual([
        "func Foo",
        "type Bar",
        "var Baz",
        "const Qux",
        "func Method",
      ]);
    });
  });

  describe("Rust", () => {
    it("captures top-level fn, struct, enum, trait, mod, type, const, static", () => {
      const src = lines(
        [
          "pub fn foo() {}",
          "fn bar() {}",
          "struct Baz;",
          "pub struct Qux { x: i32 }",
          "enum Variant { A, B }",
          "trait MyTrait {}",
          "mod nested {}",
          "type Alias = u32;",
          "const C: u32 = 1;",
          "static S: u32 = 2;",
          "pub async unsafe fn raw() {}",
        ].join("\n"),
      );
      const out = extractOutline("a.rs", src);
      expect(out.map((e) => e.text)).toEqual([
        "fn foo",
        "fn bar",
        "struct Baz",
        "struct Qux",
        "enum Variant",
        "trait MyTrait",
        "mod nested",
        "type Alias",
        "const C",
        "static S",
        "fn raw",
      ]);
    });

    it("captures impl blocks with their target type", () => {
      const src = lines(
        [
          "impl Foo {",
          "  pub fn new() -> Self {}",
          "}",
          "impl<T> Bar<T> {}",
          "impl Display for Foo {}",
        ].join("\n"),
      );
      const out = extractOutline("a.rs", src);
      expect(out.map((e) => e.text)).toEqual(["impl Foo", "impl Bar", "impl Foo"]);
    });
  });

  describe("Markdown", () => {
    it("captures headings at all levels", () => {
      const src = lines(["# Top", "", "## Sub", "", "### Sub-sub", "", "#### Deeper"].join("\n"));
      const out = extractOutline("a.md", src);
      expect(out).toEqual([
        { line: 1, text: "# Top" },
        { line: 3, text: "## Sub" },
        { line: 5, text: "### Sub-sub" },
        { line: 7, text: "#### Deeper" },
      ]);
    });

    it("skips headings inside fenced code blocks", () => {
      const src = lines(
        ["# Real heading", "", "```", "# Not a heading", "```", "", "## Also real"].join("\n"),
      );
      const out = extractOutline("a.md", src);
      expect(out).toEqual([
        { line: 1, text: "# Real heading" },
        { line: 7, text: "## Also real" },
      ]);
    });
  });

  describe("Protobuf", () => {
    it("captures top-level message / service / enum + nested rpc", () => {
      const src = lines(
        [
          'syntax = "proto3";',
          "package demo;",
          "",
          "message User {",
          "  string id = 1;",
          "}",
          "",
          "enum Status { OK = 0; }",
          "",
          "service Accounts {",
          "  rpc Get(GetReq) returns (User);",
          "  rpc List(ListReq) returns (ListResp);",
          "}",
        ].join("\n"),
      );
      const out = extractOutline("demo.proto", src);
      expect(out.map((e) => e.text)).toEqual([
        "message User",
        "enum Status",
        "service Accounts",
        "rpc Get",
        "rpc List",
      ]);
    });
  });

  describe("Plain text — novel / document chapter markers", () => {
    it("captures Chinese chapter, volume, prologue markers", () => {
      const src = lines(
        ["楔子", "今天天气不错。", "第一章 启程", "故事开始。", "卷二 江湖", "另一段。"].join("\n"),
      );
      const out = extractOutline("novel.txt", src);
      expect(out.map((e) => e.text)).toEqual(["楔子", "第一章 启程", "卷二 江湖"]);
    });

    it("captures English Chapter / Part markers", () => {
      const src = lines(
        ["Some intro.", "Chapter 1 The Beginning", "body.", "Part II Aftermath", "more body."].join(
          "\n",
        ),
      );
      const out = extractOutline("story.txt", src);
      expect(out.map((e) => e.text)).toEqual(["Chapter 1 The Beginning", "Part II Aftermath"]);
    });

    it("ignores prose lines that don't match a chapter pattern", () => {
      const src = lines(
        ["just some regular text", "another paragraph", "no chapters here"].join("\n"),
      );
      expect(extractOutline("plain.txt", src)).toEqual([]);
    });
  });

  describe("unknown extensions", () => {
    it("returns empty outline for unsupported file types", () => {
      const src = lines("function not_recognized() {}");
      expect(extractOutline("a.yml", src)).toEqual([]);
      expect(extractOutline("Makefile", src)).toEqual([]);
    });
  });
});

describe("formatOutline", () => {
  it("returns empty string for empty entries", () => {
    expect(formatOutline([])).toBe("");
  });

  it("formats single-entry outline without elision", () => {
    const result = formatOutline([{ line: 5, text: "def foo" }]);
    expect(result).toBe("[outline: 1 symbol]\n  L5  def foo");
  });

  it("singularises header for one entry, pluralises otherwise", () => {
    expect(formatOutline([{ line: 1, text: "a" }])).toMatch(/^\[outline: 1 symbol\]/);
    expect(
      formatOutline([
        { line: 1, text: "a" },
        { line: 2, text: "b" },
      ]),
    ).toMatch(/^\[outline: 2 symbols\]/);
  });

  it("right-pads line numbers to the widest entry", () => {
    const result = formatOutline([
      { line: 5, text: "a" },
      { line: 123, text: "b" },
    ]);
    // 'L  5' aligned under 'L123' — pad is to 3-wide.
    expect(result).toContain("L  5  a");
    expect(result).toContain("L123  b");
  });

  it("elides middle entries when over the 30-entry cap", () => {
    const entries = Array.from({ length: 35 }, (_, i) => ({
      line: i + 1,
      text: `sym${i + 1}`,
    }));
    const result = formatOutline(entries);
    expect(result).toMatch(/^\[outline: 35 symbols\]/);
    expect(result).toContain("sym1");
    expect(result).toContain("sym25");
    expect(result).not.toContain("sym26");
    expect(result).not.toContain("sym30");
    expect(result).toContain("sym31");
    expect(result).toContain("sym35");
    expect(result).toContain("[… 5 more symbols between L25 and L31 …]");
  });
});
