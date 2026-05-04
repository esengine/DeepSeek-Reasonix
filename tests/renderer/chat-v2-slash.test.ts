import { describe, expect, it } from "vitest";
import { isSlashInput, runSlash } from "../../src/cli/ui/chat-v2-slash.js";
import type { AgentState } from "../../src/cli/ui/state/state.js";

function blankState(): AgentState {
  return {
    lang: "en",
    session: { id: "test", branch: "main", workspace: "(test)", model: "deepseek-chat" },
    cards: [],
    composer: { value: "", cursor: 0, picker: null, shell: false, abortedHint: false },
    status: {
      mode: "auto",
      network: "online",
      cost: 0.001,
      sessionCost: 0.005,
      cacheHit: 0.6,
    },
    focusedCardId: null,
    toasts: [],
    turnInProgress: false,
  };
}

describe("isSlashInput", () => {
  it("returns true for /foo and false for foo", () => {
    expect(isSlashInput("/help")).toBe(true);
    expect(isSlashInput("  /help")).toBe(true);
    expect(isSlashInput("hello")).toBe(false);
    expect(isSlashInput("")).toBe(false);
  });
});

describe("runSlash — known commands", () => {
  it("/help dispatches a synthetic user + reply with the command list", () => {
    const out = runSlash("/help", { state: blankState() });
    const userEv = out.events.find((e) => e.type === "user.submit");
    expect(userEv && userEv.type === "user.submit" ? userEv.text : null).toBe("/help");
    const chunkEvs = out.events.filter((e) => e.type === "streaming.chunk");
    expect(chunkEvs.length).toBeGreaterThan(0);
    const text = chunkEvs.map((e) => (e.type === "streaming.chunk" ? e.text : "")).join("");
    expect(text).toContain("/help");
    expect(text).toContain("/clear");
    expect(text).toContain("/cost");
    expect(out.exit).toBeFalsy();
  });

  it("/clear dispatches session.reset", () => {
    const out = runSlash("/clear", { state: blankState() });
    expect(out.events).toHaveLength(1);
    expect(out.events[0]?.type).toBe("session.reset");
  });

  it("/cost shows the current usage in the reply text", () => {
    const out = runSlash("/cost", { state: blankState() });
    const text = out.events
      .filter((e) => e.type === "streaming.chunk")
      .map((e) => (e.type === "streaming.chunk" ? e.text : ""))
      .join("");
    expect(text).toContain("$0.00100");
    expect(text).toContain("$0.00500");
    expect(text).toContain("60%");
  });

  it("/exit returns exit:true with no dispatched events", () => {
    const out = runSlash("/exit", { state: blankState() });
    expect(out.exit).toBe(true);
    expect(out.events).toHaveLength(0);
  });

  it("/new resets the session", () => {
    const out = runSlash("/new", { state: blankState() });
    expect(out.events.some((e) => e.type === "session.reset")).toBe(true);
  });
});

describe("runSlash — unknown commands", () => {
  it("dispatches an error live card", () => {
    const out = runSlash("/banana", { state: blankState() });
    const live = out.events.find((e) => e.type === "live.show");
    if (live?.type !== "live.show") throw new Error("expected live.show");
    expect(live.tone).toBe("err");
    expect(live.text).toContain("/banana");
  });
});
