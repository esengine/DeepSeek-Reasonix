import { promises as fs } from "node:fs";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { ToolRegistry } from "../src/tools.js";
import { registerFilesystemTools } from "../src/tools/filesystem.js";
import { ReadTracker } from "../src/tools/read-tracker.js";

describe("read-before-edit gate", () => {
  let root: string;
  let tools: ToolRegistry;
  let readTracker: ReadTracker;

  beforeEach(async () => {
    root = await mkdtemp(join(tmpdir(), "reasonix-rbeg-"));
    tools = new ToolRegistry();
    registerFilesystemTools(tools, { rootDir: root });
    readTracker = new ReadTracker();
    await fs.writeFile(join(root, "hello.txt"), "alpha\nbeta\ngamma\n");
    await fs.writeFile(join(root, "other.txt"), "one\ntwo\nthree\n");
  });

  afterEach(async () => {
    await rm(root, { recursive: true, force: true });
  });

  it("edit_file refuses an unread file with a read_file hint", async () => {
    const out = await tools.dispatch(
      "edit_file",
      JSON.stringify({ path: "hello.txt", search: "alpha", replace: "ALPHA" }),
      { readTracker },
    );
    expect(out).toMatch(/read_file first/);
    expect(out).toMatch(/hello\.txt/);
    const after = await fs.readFile(join(root, "hello.txt"), "utf8");
    expect(after).toContain("alpha");
    expect(after).not.toContain("ALPHA");
  });

  it("edit_file succeeds after read_file marks the path", async () => {
    await tools.dispatch("read_file", JSON.stringify({ path: "hello.txt" }), { readTracker });
    const out = await tools.dispatch(
      "edit_file",
      JSON.stringify({ path: "hello.txt", search: "alpha", replace: "ALPHA" }),
      { readTracker },
    );
    expect(out).toMatch(/edited hello\.txt/);
    const after = await fs.readFile(join(root, "hello.txt"), "utf8");
    expect(after).toContain("ALPHA");
  });

  it("write_file counts as a read for that path", async () => {
    await tools.dispatch(
      "write_file",
      JSON.stringify({ path: "new.txt", content: "first line\nsecond line\n" }),
      { readTracker },
    );
    const out = await tools.dispatch(
      "edit_file",
      JSON.stringify({ path: "new.txt", search: "first line", replace: "FIRST LINE" }),
      { readTracker },
    );
    expect(out).toMatch(/edited new\.txt/);
  });

  it("read_file with range or head still marks the path (partial read accepted)", async () => {
    await tools.dispatch("read_file", JSON.stringify({ path: "hello.txt", head: 1 }), {
      readTracker,
    });
    const out = await tools.dispatch(
      "edit_file",
      JSON.stringify({ path: "hello.txt", search: "alpha", replace: "ALPHA" }),
      { readTracker },
    );
    expect(out).toMatch(/edited hello\.txt/);
  });

  it("multi_edit refuses the whole batch when any target is unread", async () => {
    await tools.dispatch("read_file", JSON.stringify({ path: "hello.txt" }), { readTracker });
    const out = await tools.dispatch(
      "multi_edit",
      JSON.stringify({
        edits: [
          { path: "hello.txt", search: "alpha", replace: "ALPHA" },
          { path: "other.txt", search: "one", replace: "ONE" },
        ],
      }),
      { readTracker },
    );
    expect(out).toMatch(/read_file first/);
    expect(out).toMatch(/other\.txt/);
    const hello = await fs.readFile(join(root, "hello.txt"), "utf8");
    expect(hello).toContain("alpha");
    const other = await fs.readFile(join(root, "other.txt"), "utf8");
    expect(other).toContain("one");
  });

  it("multi_edit succeeds when every target was read first", async () => {
    await tools.dispatch("read_file", JSON.stringify({ path: "hello.txt" }), { readTracker });
    await tools.dispatch("read_file", JSON.stringify({ path: "other.txt" }), { readTracker });
    const out = await tools.dispatch(
      "multi_edit",
      JSON.stringify({
        edits: [
          { path: "hello.txt", search: "alpha", replace: "ALPHA" },
          { path: "other.txt", search: "one", replace: "ONE" },
        ],
      }),
      { readTracker },
    );
    expect(out).toMatch(/applied 2 edits across 2 files/);
  });

  it("a 2nd identical unread edit gets the sharper stop-retrying hint", async () => {
    const first = await tools.dispatch(
      "edit_file",
      JSON.stringify({ path: "hello.txt", search: "alpha", replace: "ALPHA" }),
      { readTracker },
    );
    expect(first).toMatch(/read_file first/);
    const second = await tools.dispatch(
      "edit_file",
      JSON.stringify({ path: "hello.txt", search: "alpha", replace: "ALPHA" }),
      { readTracker },
    );
    expect(second).toMatch(/do not retry identical args/i);
    expect(second).toMatch(/Call read_file on the target path first/);
  });

  it("ReadTracker.reset clears the seen set so a fresh edit needs a fresh read", async () => {
    await tools.dispatch("read_file", JSON.stringify({ path: "hello.txt" }), { readTracker });
    readTracker.reset();
    const out = await tools.dispatch(
      "edit_file",
      JSON.stringify({ path: "hello.txt", search: "alpha", replace: "ALPHA" }),
      { readTracker },
    );
    expect(out).toMatch(/read_file first/);
  });

  it("with no readTracker injected, edits proceed (backwards-compatible)", async () => {
    const out = await tools.dispatch(
      "edit_file",
      JSON.stringify({ path: "hello.txt", search: "alpha", replace: "ALPHA" }),
    );
    expect(out).toMatch(/edited hello\.txt/);
  });
});

describe("ReadTracker path normalization", () => {
  it("treats different relative spellings of the same absolute path as the same file", () => {
    const tracker = new ReadTracker();
    const abs = process.platform === "win32" ? "C:\\foo\\bar.txt" : "/foo/bar.txt";
    tracker.markRead(abs);
    expect(tracker.hasRead(abs)).toBe(true);
    if (process.platform === "win32") {
      expect(tracker.hasRead(abs.toUpperCase())).toBe(true);
      expect(tracker.hasRead(abs.toLowerCase())).toBe(true);
    }
  });

  it("size reflects unique paths only", () => {
    const tracker = new ReadTracker();
    tracker.markRead("/a/b.ts");
    tracker.markRead("/a/b.ts");
    tracker.markRead("/a/c.ts");
    expect(tracker.size).toBe(2);
  });
});
