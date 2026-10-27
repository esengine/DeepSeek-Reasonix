import { afterEach, expect, it, vi } from "vitest";
import { SsePort } from "./sse";

afterEach(() => vi.unstubAllGlobals());

it("uses the handoff route so older servers cannot silently discard selected context", async () => {
  const fetch = vi.fn(async (_url: string, _init: RequestInit) => ({ ok: true, status: 204, text: async () => "" }));
  vi.stubGlobal("fetch", fetch);
  const port = new SsePort("", "r1");
  await port.steerSubagent("sa_1", "plain");
  await port.steerSubagent("sa_1", "", { summary: "selected", resultRefs: ["sa_2"] });
  expect(fetch.mock.calls[0][0]).toContain("/subagents/sa_1/steer");
  expect(JSON.parse(String(fetch.mock.calls[0][1].body))).toEqual({ text: "plain" });
  expect(fetch.mock.calls[1][0]).toContain("/subagents/sa_1/handoff");
  expect(JSON.parse(String(fetch.mock.calls[1][1].body))).toEqual({ text: "", context: { summary: "selected", resultRefs: ["sa_2"] } });
});
