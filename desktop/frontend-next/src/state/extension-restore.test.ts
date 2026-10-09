import { describe, expect, it } from "vitest";
import { fromHistory, initialState, reduce, type SessionEvent } from "./session";
import type { ExtensionSurface } from "../port/wire";

const restore = (): SessionEvent => ({ kind: "__restore", ...fromHistory([
  { role: "user", content: "recorded request" },
  { role: "assistant", content: "recorded answer" },
]) });

const publish = (extension: ExtensionSurface): SessionEvent => ({
  kind: extension.kind === "status" ? "extension_status" : "extension_surface", extension,
});

const surfaces: ExtensionSurface[] = [
  { pluginId: "fixture", surfaceId: "status", sessionId: "s1", generation: 3, kind: "status", status: { label: "Ready" } },
  { pluginId: "fixture", surfaceId: "card", sessionId: "s1", generation: 3, kind: "card", card: { title: "Result", text: "Ready" } },
  { pluginId: "fixture", surfaceId: "form", sessionId: "s1", generation: 3, kind: "form", form: { fields: [{ key: "name", kind: "input" }] } },
  { pluginId: "fixture", surfaceId: "notification", sessionId: "s1", generation: 3, kind: "notification", notification: { title: "Ready" } },
];

describe("extension publications during transcript recovery", () => {
  it.each(surfaces)("retains the accepted $kind surface and its identity", (ext) => {
    let s = reduce(initialState, { kind: "__user", text: "stale transcript", pending: false });
    s = reduce(s, publish(ext));
    const item = s.items.find((i) => i.t === "extension")!;
    s = reduce(s, restore());
    expect(s.items.filter((i) => i.t === "extension")).toEqual([item]);
    expect(s.items.some((i) => i.t === "user" && i.text === "stale transcript")).toBe(false);
    expect(s.items.some((i) => i.t === "say" && i.text === "recorded answer")).toBe(true);
    expect(s.entranceOwed).toEqual([]);
    s = reduce(s, restore());
    expect(s.items.filter((i) => i.t === "extension")).toEqual([item]);
    const updated = { ...ext };
    if (updated.status) updated.status = { label: "Updated" };
    if (updated.card) updated.card = { title: "Updated" };
    if (updated.form) updated.form = { ...updated.form, title: "Updated" };
    if (updated.notification) updated.notification = { title: "Updated" };
    s = reduce(s, publish(updated));
    expect(s.items.filter((i) => i.t === "extension")).toEqual([{ ...item, ext: updated }]);
    expect(s.entranceOwed).toEqual([]);
  });

  it("keeps pending questions and approvals while replacing sealed prompts", () => {
    let s = reduce(initialState, publish(surfaces[1]));
    s = reduce(s, { kind: "ask_request", ask: { id: "answered", questions: [] } });
    const answered = s.items.find((i) => i.t === "ask")!;
    s = reduce(s, { kind: "__decided", id: answered.id, answers: [] });
    s = reduce(s, { kind: "ask_request", ask: { id: "pending", questions: [] } });
    s = reduce(s, { kind: "approval_request", approval: { id: "approval", tool: "bash", subject: "pwd" } });
    const kept = s.items.filter((i) => i.t === "extension" || (i.t === "ask" && i.ask.id === "pending") || i.t === "approval");
    s = reduce(s, restore());
    expect(s.items.slice(-kept.length)).toEqual(kept);
    expect(s.items.some((i) => i.id === answered.id)).toBe(false);
  });
});
