import { describe, expect, it, vi } from "vitest";
import { handleSlash } from "../src/cli/ui/slash.js";
import { DeepSeekClient } from "../src/client.js";
import { savePreset, saveReasoningEffort } from "../src/config.js";
import { CacheFirstLoop } from "../src/loop.js";
import { ImmutablePrefix } from "../src/memory/runtime.js";

vi.mock("../src/config.js", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../src/config.js")>();
  return {
    ...actual,
    saveReasoningEffort: vi.fn(),
    savePreset: vi.fn(),
  };
});

function makeLoop() {
  const client = new DeepSeekClient({
    apiKey: "sk-test",
    fetch: vi.fn() as unknown as typeof fetch,
  });
  return new CacheFirstLoop({
    client,
    prefix: new ImmutablePrefix({ system: "s" }),
  });
}

describe("/preset apply — issue #770", () => {
  it("does not write reasoningEffort to disk when switching preset", () => {
    vi.mocked(saveReasoningEffort).mockClear();
    vi.mocked(savePreset).mockClear();
    for (const name of ["auto", "flash", "pro"] as const) {
      handleSlash("preset", [name], makeLoop());
    }
    expect(saveReasoningEffort).not.toHaveBeenCalled();
    expect(vi.mocked(savePreset).mock.calls.map((c) => c[0])).toEqual(["auto", "flash", "pro"]);
  });

  it("still flips the live loop's reasoningEffort when preset is applied", () => {
    vi.mocked(saveReasoningEffort).mockClear();
    vi.mocked(savePreset).mockClear();
    const loop = makeLoop();
    handleSlash("preset", ["pro"], loop);
    expect(loop.model).toBe("deepseek-v4-pro");
    expect(loop.reasoningEffort).toBe("max");
  });
});
