import { promises as fs } from "node:fs";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { renderSemanticStatus } from "../src/cli/ui/slash/handlers/semantic.js";
import { resetLocaleCache } from "../src/index/semantic/i18n.js";

describe("/semantic status renderer", () => {
  let root: string;

  beforeEach(async () => {
    process.env.REASONIX_LANG = "en";
    resetLocaleCache();
    root = await mkdtemp(join(tmpdir(), "reasonix-slash-"));
  });

  afterEach(async () => {
    await rm(root, { recursive: true, force: true });
    process.env.REASONIX_LANG = undefined;
    resetLocaleCache();
  });

  it("reports 'enabled' with chunk + file count when an index exists", async () => {
    const dir = join(root, ".reasonix", "semantic");
    await fs.mkdir(dir, { recursive: true });
    await fs.writeFile(
      join(dir, "index.meta.json"),
      JSON.stringify({
        version: 1,
        provider: "openai-compat",
        model: "text-embedding-3-small",
        dim: 768,
        updatedAt: new Date().toISOString(),
      }),
      "utf8",
    );
    await fs.writeFile(
      join(dir, "index.jsonl"),
      [
        JSON.stringify({ p: "src/a.ts", s: 1, e: 30, m: 0, t: "x", v: "" }),
        JSON.stringify({ p: "src/b.ts", s: 1, e: 30, m: 0, t: "y", v: "" }),
        JSON.stringify({ p: "src/a.ts", s: 31, e: 60, m: 0, t: "z", v: "" }),
      ].join("\n"),
      "utf8",
    );
    const out = await renderSemanticStatus(root);
    expect(out).toMatch(/enabled/);
    expect(out).toContain("3 chunks"); // 3 lines
    expect(out).toContain("2 files"); // a.ts + b.ts
    expect(out).toContain("openai-compat");
    expect(out).toContain("text-embedding-3-small");
  });

  it("reports 'no index built yet' with how-to-build hint when nothing is set up", async () => {
    const out = await renderSemanticStatus(root);
    expect(out).toMatch(/no index built/);
    expect(out).toContain("reasonix index");
  });

  it("renders Chinese under zh locale", async () => {
    process.env.REASONIX_LANG = "zh";
    resetLocaleCache();
    const out = await renderSemanticStatus(root);
    expect(out).toMatch(/状态/); // header in Chinese
    expect(out).toMatch(/还没有索引/);
  });
});
