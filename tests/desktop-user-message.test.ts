import { describe, expect, it } from "vitest";
import { type ChatMessage, applyIncoming } from "../desktop/src/App";
import type { IncomingEvent } from "../desktop/src/protocol";

function makeState(messages: ChatMessage[] = []): Parameters<typeof applyIncoming>[0] {
  return {
    ready: true,
    needsSetup: false,
    busy: false,
    model: "deepseek-v4-flash",
    currentSession: "demo",
    messages,
    pendingConfirms: [],
    pendingPathAccess: [],
    pendingChoices: [],
    pendingPlans: [],
    pendingCheckpoints: [],
    pendingRevisions: [],
    activePlan: null,
    usage: {
      totalCostUsd: 0,
      totalPromptTokens: 0,
      totalCompletionTokens: 0,
      cacheHitTokens: 0,
      cacheMissTokens: 0,
      lastCallCacheHit: null,
      lastCallCacheMiss: null,
      reservedTokens: 0,
    },
    sessions: [],
    settings: null,
    qq: null,
    balance: null,
    mentionResults: null,
    mentionPreview: null,
    mcpSpecs: [],
    mcpBridged: false,
    skills: [],
    sessionFiles: [],
    memory: [],
    jobs: [],
    activeSkill: null,
    queuedSends: [],
    retryNonce: 0,
  };
}

describe("desktop incoming QQ/user message rendering", () => {
  it("appends remote user.message into the desktop transcript and marks the tab busy", () => {
    const state = makeState([{ kind: "assistant", turn: 1, segments: [], pending: false }]);
    const next = applyIncoming(state, {
      type: "user.message",
      id: 42,
      ts: "2026-05-19T12:00:00Z",
      turn: 0,
      text: "hello from qq",
    } as IncomingEvent);

    expect(next.busy).toBe(true);
    expect(next.messages.at(-1)).toEqual({
      kind: "user",
      text: "hello from qq",
      clientId: "remote-42",
      turn: 2,
    });
  });
});
