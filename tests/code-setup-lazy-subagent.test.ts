import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { buildCodeToolset } from "../src/code/setup.js";

// #700-followup: buildCodeToolset used to eagerly construct a DeepSeekClient
// for the subagent runner, which threw "DEEPSEEK_API_KEY is not set" before
// the wizard could prompt. Now the client is constructed lazily on the first
// subagent dispatch, so the toolset builds without a key.

describe("buildCodeToolset", () => {
  let savedKey: string | undefined;
  let tmpRoot: string;

  beforeEach(() => {
    savedKey = process.env.DEEPSEEK_API_KEY;
    // biome-ignore lint/performance/noDelete: setting to "undefined" string would mask test
    delete process.env.DEEPSEEK_API_KEY;
    tmpRoot = mkdtempSync(join(tmpdir(), "reasonix-code-setup-"));
  });

  afterEach(async () => {
    if (savedKey !== undefined) process.env.DEEPSEEK_API_KEY = savedKey;
    rmSync(tmpRoot, { recursive: true, force: true });
  });

  it("builds without DEEPSEEK_API_KEY set", async () => {
    const toolset = await buildCodeToolset({ rootDir: tmpRoot });
    expect(toolset.tools.size).toBeGreaterThan(0);
    await toolset.jobs.shutdown();
  });
});
