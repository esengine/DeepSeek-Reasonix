import { afterEach, expect, it, vi } from "vitest";

afterEach(() => vi.unstubAllGlobals());

async function asked(call: (port: { models(answers?: "chat" | "decision" | "all"): Promise<unknown[]> }) => Promise<unknown>) {
  vi.resetModules();
  const urls: string[] = [];
  vi.stubGlobal("window", {});
  vi.stubGlobal("fetch", async (url: string) => {
    urls.push(url);
    return { ok: true, json: async () => ({ models: [] }) };
  });
  const { SsePort } = await import("./sse");
  await call(new SsePort("", "r1"));
  return urls;
}

it("names no scope unless asked, so the kernel's default list is the conversation models", async () => {
  expect(await asked((p) => p.models())).toEqual(["/models"]);
});

it("passes the scope it was asked for", async () => {
  expect(await asked((p) => p.models("all"))).toEqual(["/models?answers=all"]);
  expect(await asked((p) => p.models("decision"))).toEqual(["/models?answers=decision"]);
});
