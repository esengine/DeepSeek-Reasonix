import { promises as fs } from "node:fs";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { bootstrapSemanticSearchInCodeMode } from "../src/index/semantic/tool.js";
import { ToolRegistry } from "../src/tools.js";

describe("bootstrapSemanticSearchInCodeMode", () => {
  let root: string;
  let tools: ToolRegistry;

  beforeEach(async () => {
    root = await mkdtemp(join(tmpdir(), "reasonix-bootstrap-"));
    tools = new ToolRegistry();
  });

  afterEach(async () => {
    await rm(root, { recursive: true, force: true });
  });

  it("registers the tool when an index already exists", async () => {
    const semanticDir = join(root, ".reasonix", "semantic");
    await fs.mkdir(semanticDir, { recursive: true });
    await fs.writeFile(
      join(semanticDir, "index.meta.json"),
      JSON.stringify({
        version: 1,
        model: "nomic-embed-text",
        dim: 768,
        updatedAt: new Date().toISOString(),
      }),
      "utf8",
    );
    await fs.writeFile(join(semanticDir, "index.jsonl"), "", "utf8");

    const result = await bootstrapSemanticSearchInCodeMode(tools, root, {
      provider: "ollama",
      model: "nomic-embed-text",
    });
    expect(result.enabled).toBe(true);
    expect(tools.get("semantic_search")).toBeDefined();
  });

  it("skips the tool when the on-disk index targets a different provider", async () => {
    const semanticDir = join(root, ".reasonix", "semantic");
    await fs.mkdir(semanticDir, { recursive: true });
    await fs.writeFile(
      join(semanticDir, "index.meta.json"),
      JSON.stringify({
        version: 1,
        provider: "ollama",
        model: "nomic-embed-text",
        dim: 768,
        updatedAt: new Date().toISOString(),
      }),
      "utf8",
    );
    await fs.writeFile(join(semanticDir, "index.jsonl"), "", "utf8");

    const result = await bootstrapSemanticSearchInCodeMode(tools, root, {
      provider: "openai-compat",
      model: "bge-m3",
    });
    expect(result.enabled).toBe(false);
    expect(tools.get("semantic_search")).toBeUndefined();
  });

  it("silently skips (no prompt) when no index is built — even with Ollama present", async () => {
    // The contract: bootstrap NEVER prompts at startup, regardless of
    // local Ollama state. Setup happens via the explicit
    // `reasonix index` command + `/semantic` slash. This is the
    // load-bearing UX guarantee — `npx reasonix code` must be silent
    // for users who haven't opted in.
    const result = await bootstrapSemanticSearchInCodeMode(tools, root);
    expect(result.enabled).toBe(false);
    expect(tools.get("semantic_search")).toBeUndefined();
  });
});
