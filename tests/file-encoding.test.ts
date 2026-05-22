import iconv from "iconv-lite";
import { describe, expect, it } from "vitest";
import { decodeFileBuffer, encodeFile } from "../src/code/file-encoding.js";

describe("decodeFileBuffer (#1445)", () => {
  it("decodes plain ASCII as utf8", () => {
    const buf = Buffer.from("hello world\n", "utf8");
    expect(decodeFileBuffer(buf)).toEqual({ text: "hello world\n", encoding: "utf8" });
  });

  it("decodes valid UTF-8 multibyte as utf8", () => {
    const buf = Buffer.from("你好世界\n", "utf8");
    expect(decodeFileBuffer(buf)).toEqual({ text: "你好世界\n", encoding: "utf8" });
  });

  it("strips UTF-8 BOM and reports utf8-bom encoding", () => {
    const bom = Buffer.from([0xef, 0xbb, 0xbf]);
    const buf = Buffer.concat([bom, Buffer.from("// hi 你好\n", "utf8")]);
    expect(decodeFileBuffer(buf)).toEqual({ text: "// hi 你好\n", encoding: "utf8-bom" });
  });

  it("falls back to gb18030 for invalid-UTF-8 CN bytes (any platform)", () => {
    const buf = iconv.encode("你好世界\n", "gb18030");
    expect(decodeFileBuffer(buf)).toEqual({ text: "你好世界\n", encoding: "gb18030" });
  });

  it("returns empty file as utf8", () => {
    expect(decodeFileBuffer(Buffer.alloc(0))).toEqual({ text: "", encoding: "utf8" });
  });
});

describe("encodeFile (#1445)", () => {
  it("round-trips utf8", () => {
    const text = "hello 你好\n";
    expect(decodeFileBuffer(encodeFile(text, "utf8"))).toEqual({ text, encoding: "utf8" });
  });

  it("round-trips utf8-bom preserving the BOM bytes", () => {
    const text = "// hi 你好\n";
    const buf = encodeFile(text, "utf8-bom");
    expect(buf[0]).toBe(0xef);
    expect(buf[1]).toBe(0xbb);
    expect(buf[2]).toBe(0xbf);
    expect(decodeFileBuffer(buf)).toEqual({ text, encoding: "utf8-bom" });
  });

  it("round-trips gb18030 bytes", () => {
    const text = "你好世界\n";
    const buf = encodeFile(text, "gb18030");
    expect(iconv.decode(buf, "gb18030")).toBe(text);
  });
});
