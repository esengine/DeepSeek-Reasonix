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
