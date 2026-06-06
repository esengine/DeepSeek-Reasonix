import { beforeAll, describe, expect, it, vi } from "vitest";
import type { IncomingEvent } from "../desktop/src/protocol";

vi.mock("@tauri-apps/api/core", () => ({ invoke: vi.fn() }));
vi.mock("@tauri-apps/api/event", () => ({ listen: vi.fn() }));
vi.mock("@tauri-apps/api/window", () => ({
  getCurrentWindow: vi.fn(() => ({ onCloseRequested: vi.fn() })),
}));
vi.mock("@tauri-apps/plugin-dialog", () => ({ open: vi.fn(), save: vi.fn() }));
vi.mock("@tauri-apps/plugin-process", () => ({ relaunch: vi.fn() }));
vi.mock("@tauri-apps/plugin-updater", () => ({ check: vi.fn(), Update: class {} }));
vi.mock("@tauri-apps/plugin-opener", () => ({ openUrl: vi.fn() }));
vi.mock("../desktop/src/CommandPalette", () => ({
  CommandPalette: () => null,
  Toast: () => null,
  buildCommands: vi.fn(() => []),
  useCommandPalette: vi.fn(() => ({ open: false, setOpen: vi.fn() })),
}));
vi.mock("../desktop/src/Markdown", () => ({
  WorkspaceProvider: ({ children }: { children?: unknown }) => children ?? null,
}));
vi.mock("../desktop/src/ui/thread", () => ({
  ActivePlanTaskCard: () => null,
  AssistantMsg: () => null,
  CheckpointApprovalCard: () => null,
  ChoiceApprovalCard: () => null,
  ConfirmApprovalCard: () => null,
  PathAccessApprovalCard: () => null,
  PlanApprovalCard: () => null,
  PlanBanner: () => null,
  RevisionApprovalCard: () => null,
  TurnDivider: () => null,
  UserMsg: () => null,
}));

type ChatMessage = Awaited<typeof import("../desktop/src/App")>["ChatMessage"];
type AppState = Parameters<Awaited<typeof import("../desktop/src/App")>["applyIncoming"]>[0];
type ApplyIncoming = Awaited<typeof import("../desktop/src/App")>["applyIncoming"];

let applyIncoming: ApplyIncoming;

beforeAll(async () => {
  ({ applyIncoming } = await import("../desktop/src/App"));
});

function makeState(messages: ChatMessage[] = []): AppState {
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
      liveLogTokens: 0,
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

  it("elides old heavy assistant segments during long live desktop sessions", () => {
    const big = "long-session-payload\n".repeat(500);
    const messages: ChatMessage[] = Array.from({ length: 250 }, (_, i) => ({
      kind: "assistant",
      turn: i + 1,
      pending: false,
      segments: [
        { kind: "reasoning", text: `${big}reasoning-${i}` },
        { kind: "text", text: `${big}text-${i}` },
        {
          kind: "tool",
          callId: `tool-${i}`,
          name: "read_file",
          args: JSON.stringify({ path: `file-${i}.txt`, content: `${big}args-${i}` }),
          startedAt: 0,
          result: `${big}result-${i}`,
          ok: true,
        },
      ],
    }));

    const next = applyIncoming(makeState(messages), {
      type: "model.turn.started",
      turn: 999,
      model: "deepseek-v4-flash",
    } as IncomingEvent);

    const oldest = next.messages[0];
    expect(oldest?.kind).toBe("assistant");
    if (oldest?.kind !== "assistant") return;
    expect(oldest.segments[0]?.kind).toBe("reasoning");
    expect(oldest.segments[1]?.kind).toBe("text");
    expect(oldest.segments[2]?.kind).toBe("tool");
    expect(oldest.segments[0]?.text).toMatch(/^\[elided/);
    expect(oldest.segments[1]?.text).toMatch(/^\[elided/);
    if (oldest.segments[2]?.kind === "tool") {
      expect(oldest.segments[2].args).toMatch(/^\[elided/);
      expect(oldest.segments[2].result).toMatch(/^\[elided/);
    }

    const recent = next.messages[240];
    expect(recent?.kind).toBe("assistant");
    if (recent?.kind !== "assistant") return;
    expect(recent.segments[1]?.kind).toBe("text");
    expect(recent.segments[1]?.text).toContain("text-240");
  });
});
