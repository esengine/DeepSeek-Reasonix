import assert from "node:assert/strict";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { loadRuntimeConfig, parseCredentialFile } from "./config.mjs";

test("parses named credential blocks without requiring equals signs", () => {
  const parsed = parseCredentialFile("MinerU API\nmineru-secret\n\nDeepSeek\ndeepseek-secret\n");
  assert.deepEqual(parsed, { mineru: "mineru-secret", deepseek: "deepseek-secret" });
});
test("environment variables override the credential file", async () => {
  const directory = await mkdtemp(path.join(os.tmpdir(), "intelifar-config-"));
  const file = path.join(directory, "apikey.txt");
  await writeFile(file, "MinerU\nfile-mineru\nDeepSeek\nfile-deepseek\n", "utf8");
  try {
    const config = await loadRuntimeConfig({
      keyFile: file,
      cwd: directory,
      env: { MINERU_API_KEY: "env-mineru", DEEPSEEK_API_KEY: "env-deepseek", DEEPSEEK_MODEL: "deepseek-v4-pro" },
    });
    assert.equal(config.mineruApiKey, "env-mineru");
    assert.equal(config.deepseekApiKey, "env-deepseek");
    assert.equal(config.deepseekModel, "deepseek-v4-pro");
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
});

test("missing-credential errors never include another configured secret", async () => {
  await assert.rejects(
    loadRuntimeConfig({ cwd: os.tmpdir(), env: { MINERU_API_KEY: "do-not-leak-this" } }),
    (error) => error.message === "Missing runtime credentials: DeepSeek" && !error.message.includes("do-not-leak-this"),
  );
});
