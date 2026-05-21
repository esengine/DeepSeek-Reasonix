import { beforeAll, describe, expect, it, vi } from "vitest";

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

type ReduceFn = Awaited<typeof import("../desktop/src/App")>["reduce"];
type AppState = Parameters<ReduceFn>[0];

let reduce: ReduceFn;

beforeAll(async () => {
  ({ reduce } = await import("../desktop/src/App"));
});

function makeState(): AppState {
  return {
    ready: true,
    needsSetup: false,
    busy: false,
    model: "deepseek-v4-flash",
    currentSession: "demo",
    messages: [],
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

describe("desktop push_status action (#1370)", () => {
  it("appends a status message to the transcript", () => {
    // Empty `/btw` used to silently drop the keystroke (#1370). The send()
    // handler now dispatches push_status with the usage hint so the user
    // sees what's expected instead of staring at an unchanged screen.
    const state = makeState();
    const next = reduce(state, { t: "push_status", text: "▸ /btw <question>" });
    expect(next.messages.at(-1)).toEqual({
      kind: "status",
      text: "▸ /btw <question>",
    });
    // No other state should shift.
    expect(next.busy).toBe(state.busy);
    expect(next.ready).toBe(state.ready);
  });
});

describe("desktop $btw_result reducer (#1470)", () => {
  it("clears busy and appends the answer as a status message", () => {
    // /btw flips busy=true on send (via send_user echo); the answer must
    // flip it back off or the composer stays disabled (#1470).
    const state: AppState = { ...makeState(), busy: true };
    const next = reduce(state, {
      t: "incoming",
      event: { type: "$btw_result", question: "what year is it?", answer: "2026." },
    });
    expect(next.busy).toBe(false);
    expect(next.messages.at(-1)).toEqual({
      kind: "status",
      text: "≫ btw\n2026.",
    });
  });
});

describe("desktop dismiss_error reducer (recoverable / hard error parity)", () => {
  it("removes the targeted error by id, leaves other messages untouched", () => {
    const base = makeState();
    const state: AppState = {
      ...base,
      messages: [
        { kind: "status", text: "hello" },
        { kind: "error", message: "first", id: "err-a", recoverable: true },
        { kind: "error", message: "second", id: "err-b", recoverable: false },
      ],
    };
    const next = reduce(state, { t: "dismiss_error", id: "err-a" });
    expect(next.messages).toEqual([
      { kind: "status", text: "hello" },
      { kind: "error", message: "second", id: "err-b", recoverable: false },
    ]);
  });

  it("is a no-op when the id doesn't match any error", () => {
    const base = makeState();
    const state: AppState = {
      ...base,
      messages: [{ kind: "error", message: "only one", id: "err-x" }],
    };
    const next = reduce(state, { t: "dismiss_error", id: "does-not-exist" });
    expect(next.messages).toEqual(state.messages);
  });
});

describe("desktop error events carry recoverable flag from kernel events (#1456-followup)", () => {
  it("kernel error with recoverable=true produces a recoverable=true chat message", () => {
    const state = makeState();
    const next = reduce(state, {
      t: "incoming",
      event: {
        type: "error",
        id: 99,
        ts: "2026-05-21T00:00:00Z",
        turn: 1,
        message: "repeat-loop guard tripped",
        recoverable: true,
      },
    });
    const last = next.messages.at(-1);
    expect(last?.kind).toBe("error");
    if (last?.kind === "error") {
      expect(last.recoverable).toBe(true);
      expect(typeof last.id).toBe("string");
    }
  });

  it("$error protocol event treats hard errors as non-recoverable", () => {
    const state = makeState();
    const next = reduce(state, {
      t: "incoming",
      event: { type: "$error", message: "rpc died" },
    });
    const last = next.messages.at(-1);
    expect(last?.kind).toBe("error");
    if (last?.kind === "error") {
      expect(last.recoverable).toBe(false);
    }
  });
});

describe("desktop $turn_complete reducer (#1456)", () => {
  it("clears orphaned pause-gate modals so an aborted plan card stops haunting the transcript", () => {
    // When the user aborts (e.g. presses the stop button mistaken for send)
    // mid-plan-approval, the loop unwinds and emits $turn_complete. Without
    // this clear, pendingPlans stays populated — the queued user message
    // drains next and renders ABOVE the zombie plan card (#1456).
    const state: AppState = {
      ...makeState(),
      busy: true,
      pendingPlans: [{ id: 7, plan: "## Plan\nstep 1\nstep 2", summary: "do thing" }],
      pendingConfirms: [{ id: 8, kind: "shell", command: "rm -rf /tmp/x", prompt: "?" }],
      pendingPathAccess: [{ id: 9, path: "/secret" }],
      pendingChoices: [{ id: 10, question: "?", options: [], allowCustom: false }],
      pendingCheckpoints: [
        {
          id: 11,
          stepId: "s1",
          title: "step 1",
          result: "ok",
          notes: "",
          completed: 1,
          total: 2,
        },
      ],
      pendingRevisions: [{ id: 12, reason: "blocked", remainingSteps: [] }],
    };
    const next = reduce(state, { t: "incoming", event: { type: "$turn_complete" } });
    expect(next.busy).toBe(false);
    expect(next.pendingPlans).toEqual([]);
    expect(next.pendingConfirms).toEqual([]);
    expect(next.pendingPathAccess).toEqual([]);
    expect(next.pendingChoices).toEqual([]);
    expect(next.pendingCheckpoints).toEqual([]);
    expect(next.pendingRevisions).toEqual([]);
  });
});
