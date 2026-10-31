import { afterEach, describe, expect, it, vi } from "vitest";
import { SseTeam } from "./team";

// The kernel's CSRF guard refuses any POST whose body is not JSON, so a verb
// that carries nothing still has to say it is JSON and send an object.

type Sent = { method: string; path: string; contentType?: string; body?: string };

function recording(): Sent[] {
  const sent: Sent[] = [];
  vi.stubGlobal("fetch", async (path: string, init: RequestInit) => {
    const headers = (init.headers ?? {}) as Record<string, string>;
    sent.push({ method: init.method ?? "GET", path, contentType: headers["content-type"], body: init.body as string | undefined });
    return { ok: true, status: 200, json: async () => ({}), text: async () => "" };
  });
  return sent;
}

afterEach(() => vi.unstubAllGlobals());

describe("SseTeam requests", () => {
  it("sends every POST as JSON, including the ones that carry nothing", async () => {
    const sent = recording();
    const team = new SseTeam();
    await team.openAgent("a-1");
    await team.stopAgent("a-1");
    await team.continueGroup("g-1");
    await team.stopMember("g-1", "a-1");
    await team.openMember("g-1", "a-1");
    await team.bootstrap("Chief");
    const posts = sent.filter((s) => s.method === "POST");
    expect(posts).toHaveLength(6);
    for (const p of posts) {
      expect(p.contentType, p.path).toBe("application/json");
      expect(() => JSON.parse(p.body ?? ""), p.path).not.toThrow();
    }
  });

  it("leaves a GET without a body", async () => {
    const sent = recording();
    await new SseTeam().exchanges("a-1");
    expect(sent[0]).toMatchObject({ method: "GET", body: undefined });
  });
});
