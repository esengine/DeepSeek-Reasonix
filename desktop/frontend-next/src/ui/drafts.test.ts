// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { clearDraftForSession, draftKey, readDraft, writeDraft } from "./drafts";

afterEach(() => { vi.unstubAllGlobals(); localStorage.clear(); });

describe("session draft storage", () => {
  it("does not reuse a draft across workspaces or remote hosts", () => {
    const first = draftKey("", "/repo/a", "/sessions/one.jsonl");
    writeDraft(first, "keep this");
    expect(readDraft(first)).toBe("keep this");
    expect(readDraft(draftKey("", "/repo/b", "/sessions/one.jsonl"))).toBe("");
    expect(readDraft(draftKey("host-a", "/repo/a", "/sessions/one.jsonl"))).toBe("");
  });

  it("clears only the deleted host and session", () => {
    const local = draftKey("", "/repo/a", "/sessions/one.jsonl");
    const remote = draftKey("host-a", "/repo/a", "/sessions/one.jsonl");
    writeDraft(local, "local");
    writeDraft(remote, "remote");
    clearDraftForSession("", "/sessions/one.jsonl");
    expect(readDraft(local)).toBe("");
    expect(readDraft(remote)).toBe("remote");
  });

  it("does not restore stale or oversized text and tolerates unavailable storage", () => {
    const key = draftKey("", "/repo/a", "/sessions/one.jsonl");
    writeDraft(key, "old");
    writeDraft(key, "中".repeat(12_000));
    expect(readDraft(key)).toBe("");
    vi.stubGlobal("localStorage", {
      getItem: () => { throw new Error("disabled"); },
      setItem: () => { throw new Error("disabled"); },
      removeItem: () => { throw new Error("disabled"); },
    });
    expect(() => writeDraft(key, "live text")).not.toThrow();
    expect(readDraft(key)).toBe("");
  });
});
