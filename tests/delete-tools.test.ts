import { promises as fs } from "node:fs";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { ToolRegistry } from "../src/tools.js";
import { registerFilesystemTools } from "../src/tools/filesystem.js";
import { ReadTracker } from "../src/tools/read-tracker.js";

describe("delete_range tool", () => {
  let root: string;
  let tools: ToolRegistry;
  let readTracker: ReadTracker;

  beforeEach(async () => {
    root = await mkdtemp(join(tmpdir(), "reasonix-delete-tools-"));
    tools = new ToolRegistry();
    registerFilesystemTools(tools, { rootDir: root });
    readTracker = new ReadTracker();
  });

  afterEach(async () => {
    await rm(root, { recursive: true, force: true });
  });

  it("delete_range refuses unread files, then deletes an anchored range after read_file", async () => {
    await fs.writeFile(join(root, "demo.txt"), "before\nSTART\nremove\nEND\nafter\n");

    const unread = await tools.dispatch(
      "delete_range",
      { path: "demo.txt", start_anchor: "START\n", end_anchor: "END\n" },
      { readTracker },
    );
    expect(unread).toMatch(/read_file first/);

    await tools.dispatch("read_file", { path: "demo.txt" }, { readTracker });
    const out = await tools.dispatch(
      "delete_range",
      { path: "demo.txt", start_anchor: "START\n", end_anchor: "END\n" },
      { readTracker },
    );

    expect(out).toMatch(/delete_range: deleted/);
    await expect(fs.readFile(join(root, "demo.txt"), "utf8")).resolves.toBe("before\nafter\n");
  });

  it("delete_range is a no-op when anchors are duplicated", async () => {
    await fs.writeFile(join(root, "demo.txt"), "A\nSTART\nx\nSTART\nEND\n");
    await tools.dispatch("read_file", { path: "demo.txt" }, { readTracker });

    const out = await tools.dispatch(
      "delete_range",
      { path: "demo.txt", start_anchor: "START", end_anchor: "END" },
      { readTracker },
    );

    expect(out).toMatch(/no-op/);
    await expect(fs.readFile(join(root, "demo.txt"), "utf8")).resolves.toContain("x");
  });
});

describe("delete_symbol tool", () => {
  let root: string;
  let tools: ToolRegistry;
  let readTracker: ReadTracker;

  beforeEach(async () => {
    root = await mkdtemp(join(tmpdir(), "reasonix-delete-tools-"));
    tools = new ToolRegistry();
    registerFilesystemTools(tools, { rootDir: root });
    readTracker = new ReadTracker();
  });

  afterEach(async () => {
    await rm(root, { recursive: true, force: true });
  });

  it("deletes a single TypeScript function by AST range", async () => {
    await fs.writeFile(
      join(root, "demo.ts"),
      "export function keep() {\n  return 1;\n}\n\nexport function removeMe() {\n  return 2;\n}\n",
    );
    await tools.dispatch("read_file", { path: "demo.ts" }, { readTracker });

    const out = await tools.dispatch(
      "delete_symbol",
      { path: "demo.ts", name: "removeMe", kind: "function" },
      { readTracker },
    );

    expect(out).toMatch(/delete_symbol: deleted lines/);
    const after = await fs.readFile(join(root, "demo.ts"), "utf8");
    expect(after).toContain("keep");
    expect(after).not.toContain("removeMe");
  });

  it("deletes leading decorators and JSDoc with a TypeScript symbol", async () => {
    await fs.writeFile(
      join(root, "decorated.ts"),
      [
        "export function keep() {",
        "  return 1;",
        "}",
        "",
        "/** Remove this class. */",
        "@sealed",
        "export class RemoveMe {",
        "  value = 2;",
        "}",
        "",
      ].join("\n"),
    );
    await tools.dispatch("read_file", { path: "decorated.ts" }, { readTracker });

    const out = await tools.dispatch(
      "delete_symbol",
      { path: "decorated.ts", name: "RemoveMe", kind: "class" },
      { readTracker },
    );

    expect(out).toMatch(/delete_symbol: deleted lines/);
    const after = await fs.readFile(join(root, "decorated.ts"), "utf8");
    expect(after).toContain("keep");
    expect(after).not.toContain("RemoveMe");
    expect(after).not.toContain("@sealed");
    expect(after).not.toContain("Remove this class");
  });

  it("deletes Python decorators with the symbol", async () => {
    await fs.writeFile(
      join(root, "decorated.py"),
      "def keep():\n    return 1\n\n@cached\n@traced\ndef remove_me():\n    return 2\n",
    );
    await tools.dispatch("read_file", { path: "decorated.py" }, { readTracker });

    const out = await tools.dispatch(
      "delete_symbol",
      { path: "decorated.py", name: "remove_me", kind: "function" },
      { readTracker },
    );

    expect(out).toMatch(/delete_symbol: deleted lines/);
    const after = await fs.readFile(join(root, "decorated.py"), "utf8");
    expect(after).toContain("keep");
    expect(after).not.toContain("remove_me");
    expect(after).not.toContain("@cached");
    expect(after).not.toContain("@traced");
  });
});
