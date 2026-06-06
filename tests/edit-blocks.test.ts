/** SEARCH/REPLACE parsing + application — fresh temp dir per test. */

import {
  existsSync,
  lstatSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import iconv from "iconv-lite";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
  applyEditBlock,
  applyEditBlocks,
  parseEditBlocks,
  restoreSnapshots,
  snapshotBeforeEdits,
  toWholeFileEditBlock,
} from "../src/code/edit-blocks.js";

describe("parseEditBlocks", () => {
  it("parses a single block", () => {
    const text = [
      "Here is the edit:",
      "",
      "src/foo.ts",
      "<<<<<<< SEARCH",
      "const x = 1;",
      "=======",
      "const x = 2;",
      ">>>>>>> REPLACE",
    ].join("\n");
    const blocks = parseEditBlocks(text);
    expect(blocks).toHaveLength(1);
    expect(blocks[0]).toMatchObject({
      path: "src/foo.ts",
      search: "const x = 1;",
      replace: "const x = 2;",
    });
  });

  it("parses multiple blocks in one response", () => {
    const text = [
      "src/a.ts",
      "<<<<<<< SEARCH",
      "old_a",
      "=======",
      "new_a",
      ">>>>>>> REPLACE",
      "",
      "src/b.ts",
      "<<<<<<< SEARCH",
      "old_b",
      "=======",
      "new_b",
      ">>>>>>> REPLACE",
    ].join("\n");
    const blocks = parseEditBlocks(text);
    expect(blocks).toHaveLength(2);
    expect(blocks[0]!.path).toBe("src/a.ts");
    expect(blocks[1]!.path).toBe("src/b.ts");
  });

  it("handles multi-line SEARCH and REPLACE bodies", () => {
    const text = [
      "src/foo.ts",
      "<<<<<<< SEARCH",
      "line one",
      "line two",
      "line three",
      "=======",
      "line one modified",
      "NEW line",
      "line three",
      ">>>>>>> REPLACE",
    ].join("\n");
    const [block] = parseEditBlocks(text);
    expect(block!.search).toBe("line one\nline two\nline three");
    expect(block!.replace).toBe("line one modified\nNEW line\nline three");
  });

  it("recognizes an empty SEARCH (new-file sentinel)", () => {
    const text = [
      "src/new.ts",
      "<<<<<<< SEARCH",
      "=======",
      "brand new file",
      ">>>>>>> REPLACE",
    ].join("\n");
    const [block] = parseEditBlocks(text);
    expect(block!.search).toBe("");
    expect(block!.replace).toBe("brand new file");
  });

  it("returns an empty list when there are no blocks", () => {
    expect(parseEditBlocks("just prose, no edits here")).toEqual([]);
  });

  it("ignores stray 7-char runs that aren't part of a real block", () => {
    // A JS file that happens to contain the marker string in an unrelated context.
    const text = 'const note = "<<<<<<< not an edit block";\nmore prose';
    expect(parseEditBlocks(text)).toEqual([]);
  });
});

describe("applyEditBlock", () => {
  let root: string;
  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "reasonix-edit-"));
  });
  afterEach(() => {
    rmSync(root, { recursive: true, force: true });
  });

  it("applies a simple replacement", () => {
    writeFileSync(join(root, "a.txt"), "hello world\n", "utf8");
    const result = applyEditBlock(
      { path: "a.txt", search: "hello", replace: "goodbye", offset: 0 },
      root,
    );
    expect(result.status).toBe("applied");
    expect(readFileSync(join(root, "a.txt"), "utf8")).toBe("goodbye world\n");
  });

  it("treats leading slash paths as project-root relative", () => {
    writeFileSync(join(root, "a.txt"), "hello world\n", "utf8");
    const result = applyEditBlock(
      { path: "/a.txt", search: "hello", replace: "goodbye", offset: 0 },
      root,
    );
    expect(result.status).toBe("applied");
    expect(readFileSync(join(root, "a.txt"), "utf8")).toBe("goodbye world\n");
  });

  it("allows real absolute paths when they stay inside rootDir", () => {
    const absPath = join(root, "a.txt");
    writeFileSync(absPath, "hello world\n", "utf8");
    const result = applyEditBlock(
      { path: absPath, search: "hello", replace: "goodbye", offset: 0 },
      root,
    );
    expect(result.status).toBe("applied");
    expect(readFileSync(absPath, "utf8")).toBe("goodbye world\n");
  });

  it("refuses real absolute paths outside rootDir", () => {
    const outside = resolve(root, "..", "outside.txt");
    writeFileSync(outside, "hello world\n", "utf8");
    try {
      const result = applyEditBlock(
        { path: outside, search: "hello", replace: "goodbye", offset: 0 },
        root,
      );
      expect(result.status).toBe("path-escape");
      expect(readFileSync(outside, "utf8")).toBe("hello world\n");
    } finally {
      rmSync(outside, { force: true });
    }
  });

  it("creates a new file when SEARCH is empty and file doesn't exist", () => {
    const result = applyEditBlock(
      { path: "new/nested/file.ts", search: "", replace: "export const x = 1;\n", offset: 0 },
      root,
    );
    expect(result.status).toBe("created");
    expect(existsSync(join(root, "new/nested/file.ts"))).toBe(true);
    expect(readFileSync(join(root, "new/nested/file.ts"), "utf8")).toBe("export const x = 1;\n");
  });

  it("reports not-found when SEARCH doesn't match", () => {
    writeFileSync(join(root, "a.txt"), "hello world\n", "utf8");
    const result = applyEditBlock(
      { path: "a.txt", search: "NOT THERE", replace: "x", offset: 0 },
      root,
    );
    expect(result.status).toBe("not-found");
    // File unchanged.
    expect(readFileSync(join(root, "a.txt"), "utf8")).toBe("hello world\n");
  });

  it("reports file-missing when SEARCH is non-empty and file absent", () => {
    const result = applyEditBlock(
      { path: "missing.txt", search: "something", replace: "x", offset: 0 },
      root,
    );
    expect(result.status).toBe("file-missing");
  });

  it("refuses paths that escape rootDir", () => {
    const result = applyEditBlock(
      { path: "../escape.txt", search: "", replace: "boom", offset: 0 },
      root,
    );
    expect(result.status).toBe("path-escape");
    expect(existsSync(join(root, "..", "escape.txt"))).toBe(false);
  });

  it("refuses ambiguous SEARCH text that appears twice", () => {
    writeFileSync(join(root, "a.txt"), "foo bar foo\n", "utf8");
    const result = applyEditBlock(
      { path: "a.txt", search: "foo", replace: "FOO", offset: 0 },
      root,
    );
    expect(result.status).toBe("not-found");
    expect(result.message).toMatch(/multiple times/);
    expect(readFileSync(join(root, "a.txt"), "utf8")).toBe("foo bar foo\n");
  });

  it("matches LF search against CRLF file content", () => {
    writeFileSync(join(root, "crlf.txt"), "hello\r\nworld\r\n", "utf8");
    const result = applyEditBlock(
      { path: "crlf.txt", search: "hello\nworld", replace: "goodbye\nworld", offset: 0 },
      root,
    );
    expect(result.status).toBe("applied");
    expect(readFileSync(join(root, "crlf.txt"), "utf8")).toBe("goodbye\r\nworld\r\n");
  });

  it("preserves LF line endings when file uses LF", () => {
    writeFileSync(join(root, "lf.txt"), "line1\nline2\n", "utf8");
    const result = applyEditBlock(
      { path: "lf.txt", search: "line1\nline2", replace: "LINE1\nLINE2", offset: 0 },
      root,
    );
    expect(result.status).toBe("applied");
    expect(readFileSync(join(root, "lf.txt"), "utf8")).toBe("LINE1\nLINE2\n");
  });

  it("preserves symlink targets instead of replacing the link with a regular file", () => {
    const outside = resolve(root, "..", "outside-symlink-target.txt");
    const link = join(root, "linked.txt");
    writeFileSync(outside, "hello world\n", "utf8");
    let symlinksWorked = true;
    try {
      symlinkSync(outside, link);
    } catch {
      symlinksWorked = false;
    }
    if (!symlinksWorked) {
      rmSync(outside, { force: true });
      return;
    }
    try {
      const result = applyEditBlock(
        { path: "linked.txt", search: "hello", replace: "goodbye", offset: 0 },
        root,
      );

      expect(result.status).toBe("applied");
      expect(lstatSync(link).isSymbolicLink()).toBe(true);
      expect(readFileSync(outside, "utf8")).toBe("goodbye world\n");
    } finally {
      rmSync(link, { force: true });
      rmSync(outside, { force: true });
    }
  });
});

describe("snapshotBeforeEdits + restoreSnapshots", () => {
  let root: string;
  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "reasonix-undo-"));
  });
  afterEach(() => {
    rmSync(root, { recursive: true, force: true });
  });

  it("restores a modified file to its pre-edit content", () => {
    writeFileSync(join(root, "a.txt"), "original\n", "utf8");
    const block = { path: "a.txt", search: "original", replace: "CHANGED", offset: 0 };
    const snaps = snapshotBeforeEdits([block], root);
    applyEditBlocks([block], root);
    expect(readFileSync(join(root, "a.txt"), "utf8")).toBe("CHANGED\n");

    const undoResults = restoreSnapshots(snaps, root);
    expect(undoResults).toHaveLength(1);
    expect(undoResults[0]!.status).toBe("applied");
    expect(readFileSync(join(root, "a.txt"), "utf8")).toBe("original\n");
  });

  it("deletes a file that was newly created by the edit", () => {
    const block = { path: "fresh.ts", search: "", replace: "new content", offset: 0 };
    const snaps = snapshotBeforeEdits([block], root);
    applyEditBlocks([block], root);
    expect(existsSync(join(root, "fresh.ts"))).toBe(true);

    restoreSnapshots(snaps, root);
    expect(existsSync(join(root, "fresh.ts"))).toBe(false);
  });

  it("de-duplicates per path even when a batch has multiple blocks for the same file", () => {
    writeFileSync(join(root, "a.txt"), "one two three\n", "utf8");
    const blocks = [
      { path: "a.txt", search: "one", replace: "ONE", offset: 0 },
      { path: "/a.txt", search: "two", replace: "TWO", offset: 10 },
    ];
    const snaps = snapshotBeforeEdits(blocks, root);
    expect(snaps).toHaveLength(1); // not 2 — same file
    expect(snaps[0]!.prevContent).toBe("one two three\n");
  });

  it("does not snapshot paths outside rootDir before apply rejects them", () => {
    const outside = resolve(root, "..", "outside-secret.txt");
    writeFileSync(outside, "SECRET_OUTSIDE_WORKSPACE", "utf8");
    try {
      const block = { path: outside, search: "SECRET", replace: "PUBLIC", offset: 0 };
      const snaps = snapshotBeforeEdits([block], root);
      const [result] = applyEditBlocks([block], root);

      expect(snaps).toEqual([]);
      expect(result!.status).toBe("path-escape");
      expect(readFileSync(outside, "utf8")).toBe("SECRET_OUTSIDE_WORKSPACE");
    } finally {
      rmSync(outside, { force: true });
    }
  });

  it("restores multiple files in a single batch independently", () => {
    writeFileSync(join(root, "a.txt"), "aa\n", "utf8");
    writeFileSync(join(root, "b.txt"), "bb\n", "utf8");
    const blocks = [
      { path: "a.txt", search: "aa", replace: "AAA", offset: 0 },
      { path: "b.txt", search: "bb", replace: "BBB", offset: 10 },
    ];
    const snaps = snapshotBeforeEdits(blocks, root);
    applyEditBlocks(blocks, root);

    restoreSnapshots(snaps, root);
    expect(readFileSync(join(root, "a.txt"), "utf8")).toBe("aa\n");
    expect(readFileSync(join(root, "b.txt"), "utf8")).toBe("bb\n");
  });

  it("refuses to restore a snapshot whose path escapes rootDir", () => {
    const fakeSnap = [{ path: "../escape.txt", prevContent: "boom" }];
    const results = restoreSnapshots(fakeSnap, root);
    expect(results[0]!.status).toBe("path-escape");
    expect(existsSync(join(root, "..", "escape.txt"))).toBe(false);
  });
});

describe("toWholeFileEditBlock", () => {
  let root: string;
  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "reasonix-whole-"));
  });
  afterEach(() => {
    rmSync(root, { recursive: true, force: true });
  });

  it("captures existing content as SEARCH for overwrites", () => {
    writeFileSync(join(root, "hello.txt"), "old content\n", "utf8");
    const block = toWholeFileEditBlock("hello.txt", "new content\n", root);
    expect(block.search).toBe("old content\n");
    expect(block.replace).toBe("new content\n");
    // Round-trip: applying this block swaps the whole file.
    const [res] = applyEditBlocks([block], root);
    expect(res!.status).toBe("applied");
    expect(readFileSync(join(root, "hello.txt"), "utf8")).toBe("new content\n");
  });

  it("leaves SEARCH empty when the file doesn't exist (create-new sentinel)", () => {
    const block = toWholeFileEditBlock("new.txt", "fresh\n", root);
    expect(block.search).toBe("");
    expect(block.replace).toBe("fresh\n");
    const [res] = applyEditBlocks([block], root);
    expect(res!.status).toBe("created");
    expect(readFileSync(join(root, "new.txt"), "utf8")).toBe("fresh\n");
  });
});

describe("applyEditBlocks (batch)", () => {
  let root: string;
  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "reasonix-edit-"));
  });
  afterEach(() => {
    rmSync(root, { recursive: true, force: true });
  });

  it("applies a two-file batch; each result is independent", () => {
    writeFileSync(join(root, "a.txt"), "alpha\n", "utf8");
    writeFileSync(join(root, "b.txt"), "bravo\n", "utf8");
    const results = applyEditBlocks(
      [
        { path: "a.txt", search: "alpha", replace: "ALPHA", offset: 0 },
        { path: "b.txt", search: "NOPE", replace: "X", offset: 10 },
      ],
      root,
    );
    expect(results).toHaveLength(2);
    expect(results[0]!.status).toBe("applied");
    expect(results[1]!.status).toBe("not-found");
    expect(readFileSync(join(root, "a.txt"), "utf8")).toBe("ALPHA\n");
    expect(readFileSync(join(root, "b.txt"), "utf8")).toBe("bravo\n"); // untouched
  });
});

describe("edit pipeline preserves file encoding (#1445)", () => {
  let root: string;
  beforeEach(() => {
    root = mkdtempSync(join(tmpdir(), "edit-encoding-"));
  });
  afterEach(() => {
    rmSync(root, { recursive: true, force: true });
  });

  it("edits a GB18030 file and writes back in GB18030", () => {
    const original = "标题\n旧内容\n尾部\n";
    const file = join(root, "cn.txt");
    writeFileSync(file, iconv.encode(original, "gb18030"));

    const result = applyEditBlock(
      { path: "cn.txt", search: "旧内容", replace: "新内容", offset: 0 },
      root,
    );
    expect(result.status).toBe("applied");

    const onDisk = readFileSync(file);
    expect(iconv.decode(onDisk, "gb18030")).toBe("标题\n新内容\n尾部\n");
    expect(() => new TextDecoder("utf-8", { fatal: true }).decode(onDisk)).toThrow();
  });

  it("edits a UTF-8 BOM file and preserves the BOM bytes", () => {
    const bom = Buffer.from([0xef, 0xbb, 0xbf]);
    const file = join(root, "bom.txt");
    writeFileSync(file, Buffer.concat([bom, Buffer.from("hello world\n", "utf8")]));

    const result = applyEditBlock(
      { path: "bom.txt", search: "world", replace: "你好", offset: 0 },
      root,
    );
    expect(result.status).toBe("applied");

    const onDisk = readFileSync(file);
    expect(onDisk[0]).toBe(0xef);
    expect(onDisk[1]).toBe(0xbb);
    expect(onDisk[2]).toBe(0xbf);
    expect(onDisk.subarray(3).toString("utf8")).toBe("hello 你好\n");
  });

  it("snapshot + restore round-trips through GB18030 without re-encoding", () => {
    const original = "保留编码\n";
    const file = join(root, "cn.txt");
    writeFileSync(file, iconv.encode(original, "gb18030"));

    const snaps = snapshotBeforeEdits(
      [{ path: "cn.txt", search: "x", replace: "y", offset: 0 }],
      root,
    );
    expect(snaps[0]?.prevEncoding).toBe("gb18030");

    writeFileSync(file, iconv.encode("被修改\n", "gb18030"));
    const restored = restoreSnapshots(snaps, root);
    expect(restored[0]?.status).toBe("applied");
    expect(iconv.decode(readFileSync(file), "gb18030")).toBe(original);
  });
});
