import { beforeAll, describe, expect, it, vi } from "vitest";
import { MockPort } from "./mock";
import { SsePort } from "./sse";

// The panel has always told the reader that saving keeps the version it
// replaced. Until the revisions endpoints were wired, nothing in the UI could
// show that history or step back into it, so the promise was unverifiable.
// The fixture reads a stored preference on construction. A map is enough — the
// subject here is revision history, not storage.
beforeAll(() => {
  const store = new Map<string, string>();
  vi.stubGlobal("localStorage", {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => void store.set(k, v),
    removeItem: (k: string) => void store.delete(k),
  });
});

describe("memory revisions", () => {
  it("keeps the version a save replaced", async () => {
    const port = new MockPort();
    const before = (await port.memories()).memories[0];
    await port.saveMemory({
      name: before.name, title: "改过的标题", description: before.description ?? "",
      body: "改过的正文", activation: before.activation,
    });

    const revs = await port.memoryRevisions(before.name);
    expect(revs.length).toBeGreaterThan(1);
    expect(revs[0].revision).toBe((before.revision ?? 1) + 1);
    expect(revs[0].body).toBe("改过的正文");
    expect(revs.some((r) => r.body === before.body)).toBe(true);
  });

  // Restoring appends rather than rewinds: the entry a reader was just looking
  // at has to still be there afterwards, or "restore" becomes a way to lose the
  // very revision they were comparing against.
  it("restores by writing a new revision, not by dropping later ones", async () => {
    const port = new MockPort();
    const original = (await port.memories()).memories[0];
    await port.saveMemory({
      name: original.name, title: "v2", description: "", body: "第二版正文",
      activation: original.activation,
    });
    const afterEdit = (await port.memories()).memories.find((m) => m.name === original.name)!;

    await port.restoreMemory(original.name, original.revision ?? 1);

    const now = (await port.memories()).memories.find((m) => m.name === original.name)!;
    expect(now.body).toBe(original.body);
    expect(now.revision).toBe((afterEdit.revision ?? 1) + 1);
    const revs = await port.memoryRevisions(original.name);
    expect(revs.some((r) => r.body === "第二版正文")).toBe(true);
  });
});

// The kernel already served both endpoints; only the client half was missing.
// These pin the shapes it answers to, so a rename on either side fails here
// rather than as an empty panel.
describe("the revisions client", () => {
  it("asks for one memory's history by name", async () => {
    const seen: string[] = [];
    vi.stubGlobal("fetch", async (url: string) => {
      seen.push(url);
      return { ok: true, status: 200, json: async () => ({ revisions: [{ name: "a", activation: "pinned", revision: 2 }] }),
        text: async () => "", headers: new Map([["content-type", "application/json"]]) };
    });
    const out = await new SsePort().memoryRevisions("recall-cjk & keywords");
    expect(seen[0]).toContain("/memory/revisions?name=recall-cjk%20%26%20keywords");
    expect(out).toHaveLength(1);
  });

  it("reads an empty history as no revisions rather than undefined", async () => {
    vi.stubGlobal("fetch", async () => ({
      ok: true, status: 200, json: async () => ({}), text: async () => "",
      headers: new Map([["content-type", "application/json"]]),
    }));
    expect(await new SsePort().memoryRevisions("x")).toEqual([]);
  });
});
