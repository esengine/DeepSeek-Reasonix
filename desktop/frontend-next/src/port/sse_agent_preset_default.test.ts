import { afterEach, expect, it, vi } from "vitest";
import { SsePort } from "./sse";

afterEach(() => vi.unstubAllGlobals());

// The kernel answers a default-preset change with 204 and no body, the way a
// status-only POST does. Reading that as JSON is what made a change that landed
// report "Unexpected end of JSON input".
it("treats the empty 204 answer to a default-preset change as success", async () => {
  const calls: { url: string; body: unknown }[] = [];
  vi.stubGlobal("fetch", vi.fn(async (url: string, init?: { body?: string }) => {
    calls.push({ url, body: init?.body ? JSON.parse(init.body) : undefined });
    return { ok: true, status: 204, text: async () => "", json: async () => { throw new SyntaxError("Unexpected end of JSON input"); } };
  }));
  const port = new SsePort("", "r1");
  await expect(port.setDefaultAgentPreset("user:reviewer", false)).resolves.toBeUndefined();
  await expect(port.setDefaultAgentPreset("user:reviewer", true)).resolves.toBeUndefined();
  expect(calls.map((c) => c.body)).toEqual([{ id: "user:reviewer", workspace: false }, { id: "user:reviewer", workspace: true }]);
  expect(calls.every((c) => c.url.endsWith("/agent-presets/default"))).toBe(true);
});
