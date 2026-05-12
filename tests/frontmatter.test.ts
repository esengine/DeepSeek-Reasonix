import { describe, expect, it } from "vitest";
import { parseFrontmatter } from "../src/frontmatter.js";

describe("parseFrontmatter", () => {
  it("parses a basic single-line frontmatter", () => {
    const { data, body } = parseFrontmatter("---\nname: foo\ndescription: bar\n---\nhello\n");
    expect(data).toEqual({ name: "foo", description: "bar" });
    expect(body).toBe("hello\n");
  });

  it("strips a UTF-8 BOM before the opening delimiter", () => {
    const raw = "\uFEFF---\ndescription: with bom\n---\nbody\n";
    const { data, body } = parseFrontmatter(raw);
    expect(data.description).toBe("with bom");
    expect(body).toBe("body\n");
  });

  it("folds indented continuation lines into the preceding key", () => {
    const raw = "---\ndescription: first line\n  second line\n  third line\n---\nbody\n";
    const { data } = parseFrontmatter(raw);
    expect(data.description).toBe("first line second line third line");
  });

  it("folds continuations onto a key whose value started empty", () => {
    const raw = "---\ndescription:\n  one\n  two\n---\nbody\n";
    const { data } = parseFrontmatter(raw);
    expect(data.description).toBe("one two");
  });

  it("strips wrapping double quotes from a value", () => {
    const { data } = parseFrontmatter('---\ndescription: "quoted text"\n---\nb\n');
    expect(data.description).toBe("quoted text");
  });

  it("strips wrapping single quotes from a value", () => {
    const { data } = parseFrontmatter("---\ndescription: 'quoted text'\n---\nb\n");
    expect(data.description).toBe("quoted text");
  });

  it("leaves mismatched or interior quotes alone", () => {
    const { data } = parseFrontmatter('---\ndescription: "starts but no end\n---\nb\n');
    expect(data.description).toBe('"starts but no end');
  });

  it("returns body unchanged when frontmatter delimiters are missing", () => {
    const raw = "no frontmatter here\njust text\n";
    const { data, body } = parseFrontmatter(raw);
    expect(data).toEqual({});
    expect(body).toBe(raw);
  });

  it("returns body unchanged when the closing delimiter is missing", () => {
    const raw = "---\ndescription: never closed\nmore text\n";
    const { data, body } = parseFrontmatter(raw);
    expect(data).toEqual({});
    expect(body).toBe(raw);
  });

  it("handles CRLF line endings", () => {
    const raw = "---\r\nname: x\r\ndescription: y\r\n---\r\nbody\r\n";
    const { data } = parseFrontmatter(raw);
    expect(data).toEqual({ name: "x", description: "y" });
  });

  it("resets continuation scope across a blank line inside frontmatter", () => {
    const raw = "---\ndescription: one\n\n  two\n---\nb\n";
    const { data } = parseFrontmatter(raw);
    expect(data.description).toBe("one");
  });

  it("drops keys that would mutate Object.prototype (__proto__, constructor, prototype)", () => {
    const raw =
      "---\n__proto__: polluted\nconstructor: nope\nprototype: also-nope\nname: ok\n---\n";
    const probe = {} as Record<string, unknown>;
    const before = probe.polluted;
    const { data } = parseFrontmatter(raw);
    expect(data.name).toBe("ok");
    expect(data.__proto__).toBeUndefined();
    expect(Object.getPrototypeOf(data)).toBeNull();
    expect(probe.polluted).toBe(before);
  });
});
