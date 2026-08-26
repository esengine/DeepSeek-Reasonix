// Run: tsx src/__tests__/remote-session-surface.test.tsx

import React from "react";
import { JSDOM } from "jsdom";
import { act } from "react";

import type { AppBindings } from "../lib/bridge";
import type { TabMeta } from "../lib/types";
import type { RemoteSessionApi } from "../lib/useRemoteSession";

let passed = 0;
let failed = 0;
function ok(value: boolean, label: string) {
  if (value) {
    process.stdout.write(`  PASS  ${label}\n`);
    passed += 1;
  } else {
    process.stdout.write(`  FAIL  ${label}\n`);
    failed += 1;
  }
}

console.log("\nRemote session surface + hook");
const dom = new JSDOM("<!doctype html><html><body><div id=\"root\"></div></body></html>", {
  pretendToBeVisual: true,
  url: "http://localhost/",
});
(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT: boolean }).IS_REACT_ACT_ENVIRONMENT = true;
globalThis.window = dom.window as unknown as Window & typeof globalThis;
globalThis.document = dom.window.document;
Object.defineProperty(globalThis, "navigator", { configurable: true, value: dom.window.navigator });
globalThis.Node = dom.window.Node;
globalThis.Element = dom.window.Element;
globalThis.HTMLElement = dom.window.HTMLElement;
globalThis.Event = dom.window.Event;
globalThis.KeyboardEvent = dom.window.KeyboardEvent;
globalThis.localStorage = dom.window.localStorage;
globalThis.getComputedStyle = dom.window.getComputedStyle.bind(dom.window) as typeof getComputedStyle;
const elementProto = dom.window.HTMLElement.prototype;
Object.defineProperty(elementProto, "attachEvent", { configurable: true, value: () => {} });
Object.defineProperty(elementProto, "offsetHeight", {
  configurable: true,
  get(this: HTMLElement) { return this.classList.contains("transcript") ? 800 : 40; },
});
Object.defineProperty(elementProto, "offsetWidth", { configurable: true, get: () => 800 });
Object.defineProperty(elementProto, "clientHeight", {
  configurable: true,
  get(this: HTMLElement) { return this.classList.contains("transcript") ? 800 : 40; },
});
Object.defineProperty(elementProto, "clientWidth", { configurable: true, get: () => 800 });
(elementProto as unknown as { scrollTo: (arg?: number | ScrollToOptions) => void }).scrollTo = function (
  this: HTMLElement,
  arg?: number | ScrollToOptions,
) {
  this.scrollTop = typeof arg === "number" ? arg : arg?.top ?? this.scrollTop;
};
// Transcript's virtualization calls the global rAF; jsdom only exposes it on
// the (visual) window.
globalThis.requestAnimationFrame = dom.window.requestAnimationFrame?.bind(dom.window) ?? ((cb: FrameRequestCallback) => setTimeout(() => cb(Date.now()), 16) as unknown as number);
globalThis.cancelAnimationFrame = dom.window.cancelAnimationFrame?.bind(dom.window) ?? ((handle: number) => clearTimeout(handle));
Object.defineProperty(elementProto, "detachEvent", { configurable: true, value: () => {} });

const tape: string[] = [];
let failApproval = false;
let failOpen = false;
let failHydration = true;
let statusGoalStatus: "stopped" | "complete" = "stopped";
let statusQualityFloor: "standard" | "delivery" = "standard";
let statusModelLabel = "DeepSeek · Mock";
let statusEffort = "high";
let statusPendingPrompt = false, replayedPrompts: unknown[] = [];
let snapshotHistory: unknown[] = [];
let blockApproval = false;
let releaseApproval: (() => void) | undefined;
let blockAnswer = false;
let releaseAnswer: (() => void) | undefined;
let resolveRaceSnapshot: ((value: { history: unknown[]; status: unknown }) => void) | undefined;
const resolveStateRaceSnapshots: Array<(value: { history: unknown[]; status: unknown }) => void> = [];
let rotationSnapshotCalls = 0;
let resolveRotationReconcile: ((value: { history: unknown[]; status: unknown }) => void) | undefined;
window.go = { main: { App: {
  async RemoteTabSnapshot(tabId: string) {
    tape.push(`snapshot:${tabId}`);
		if (tabId === "tab-hydration-failure" && failHydration) throw new Error("history exceeds bridge limit");
		if (tabId === "tab-pending-model") return new Promise(() => {});
		if (tabId === "tab-race") return new Promise<{ history: unknown[]; status: unknown }>((resolve) => { resolveRaceSnapshot = resolve; });
		if (tabId === "tab-state-race") return new Promise<{ history: unknown[]; status: unknown }>((resolve) => { resolveStateRaceSnapshots.push(resolve); });
		if (tabId === "tab-reconcile-rotation") {
			rotationSnapshotCalls += 1;
			if (rotationSnapshotCalls === 2) {
				return new Promise<{ history: unknown[]; status: unknown }>((resolve) => { resolveRotationReconcile = resolve; });
			}
			return {
				history: [{ role: "assistant", content: rotationSnapshotCalls === 1 ? "initial session" : rotationSnapshotCalls === 3 ? "fresh rotated session" : "fresh reconciled turn" }],
				status: { running: false, label: "Rotation", plan: false, toolApprovalMode: "ask", goal: "" },
			};
		}
		if (tabId === "tab-tool-history") return {
			history: [
				{ role: "assistant", content: "", toolCalls: [{ id: "remote-tool", name: "bash", arguments: "{\"command\":\"go test ./...\"}" }] },
				{ role: "tool", content: "remote tool output", toolCallId: "remote-tool", toolName: "bash" },
			],
			status: { running: false, label: "Tools", plan: false, toolApprovalMode: "ask", goal: "" },
		};
		if (tabId === "tab-replay") return {
			history: [], status: { label: "Replay", plan: false, toolApprovalMode: "ask", goal: "" },
			pendingEvents: [
				{ kind: "approval_request", approval: { id: "replayed-approval", tool: "bash", subject: "pending while inactive" } },
				{ kind: "extension_surface", extension: { pluginId: "replayed-plugin", surfaceId: "replayed-form", kind: "form", form: { title: "Pending form", fields: [{ key: "region", label: "Region", kind: "input" }] } } },
			],
		};
    if (tabId === "tab-status-fallback") return { history: [] };
    return {
      history: snapshotHistory,
      checkpoints: [{
        turn: 3,
        prompt: "checkpoint",
        files: ["src/main.ts"],
        fileCount: 2,
        time: 1,
        canCode: true,
        canConversation: true,
      }],
      commands: [{ name: "remote-review", description: "Review remotely", kind: "custom", group: "skills" }],
      status: {
        label: statusModelLabel,
        plan: true,
        toolApprovalMode: "auto",
        goal: "",
        goalStatus: statusGoalStatus,
        goalRuntime: { turnsUsed: 2, turnsLimit: 0, tokensUsed: 321, requestsUsed: 3, workDurationMs: 4000, tokensLimit: 0, noProgressTurns: 0, noProgressLimit: 0, budgetExtensions: 0 },
        qualityFloor: statusQualityFloor,
        effort: { supported: true, current: statusEffort, default: "auto", levels: ["auto", "high", "max"] },
        used: 1200,
        window: 64000,
        cacheHit: 800,
        cacheMiss: 400,
        lastUsage: { promptTokens: 1000, completionTokens: 200, totalTokens: 1200, cacheHitTokens: 800, cacheMissTokens: 200 },
        balance: { available: true, display: "¥88.00" },
        sessionCostQuote: { original: { amount: "0.12", currency: "CNY" }, selected: { amount: "0.12", currency: "CNY" }, estimated: false, costComplete: true, displayComplete: true, complete: true },
        jobs: [{ id: "job-remote", kind: "bash", label: "tests", status: "running", startedAt: 1 }],
      },
    };
  },
	async RemoteTabStatus(tabId: string) {
		tape.push(`status:${tabId}`);
		if (tabId === "tab-status-fallback") {
			return { running: false, plan: true, toolApprovalMode: "yolo", goal: "", qualityFloor: "standard" };
		}
		return {
			running: false, pendingPrompt: statusPendingPrompt, backgroundJobs: 0,
			label: statusModelLabel, plan: true, toolApprovalMode: "auto", goal: "",
			goalStatus: statusGoalStatus, qualityFloor: statusQualityFloor,
			goalRuntime: { turnsUsed: 2, turnsLimit: 0, tokensUsed: 321, requestsUsed: 3, workDurationMs: 4000, tokensLimit: 0, noProgressTurns: 0, noProgressLimit: 0, budgetExtensions: 0 },
			effort: { supported: true, current: statusEffort, default: "auto", levels: ["auto", "high", "max"] },
		};
	},
  async ReplayRemoteTabPrompts(tabId: string) { tape.push(`replay-prompts:${tabId}`); return replayedPrompts; },
  async SubmitRemoteTab(tabId: string, text: string) {
    tape.push(`submit:${tabId}:${text}`);
  },
  async CancelRemoteTab(tabId: string) {
    tape.push(`cancel:${tabId}`);
  },
  async RewindRemoteTab(tabId: string, checkpointId: string, scope: string) {
    tape.push(`rewind:${tabId}:${checkpointId}:${scope}`);
  },
  async ForkRemoteTab(tabId: string, turn: number, name: string) {
    tape.push(`fork:${tabId}:${turn}:${name}`);
  },
  async SummarizeRemoteTab(tabId: string, turn: number, mode: string) {
    tape.push(`summarize:${tabId}:${turn}:${mode}`);
  },
  async CompactRemoteTab(tabId: string, instructions: string) { tape.push(`compact:${tabId}:${instructions}`); },
  async SetRemoteTabEffort(tabId: string, level: string) {
    tape.push(`effort:${tabId}:${level}`);
    statusEffort = level;
  },
  async SetRemoteTabModel(tabId: string, ref: string) {
    tape.push(`model:${tabId}:${ref}`);
    statusModelLabel = `Model · ${ref}`;
    statusEffort = "max";
  },
  async SetRemoteTabQualityFloor(tabId: string, floor: string) {
    tape.push(`quality-floor:${tabId}:${floor}`);
    statusQualityFloor = floor === "delivery" ? "delivery" : "standard";
  },
  async PauseRemoteTabGoal(tabId: string) {
    tape.push(`pause-goal:${tabId}`);
  },
  async ResumeRemoteTabGoal(tabId: string) {
    tape.push(`resume-goal:${tabId}`);
  },
  async SteerRemoteTab(tabId: string, input: string) {
    tape.push(`steer:${tabId}:${input}`);
  },
  async CancelRemoteTabJobs(tabId: string, jobIds: string[]) {
    tape.push(`cancel-jobs:${tabId}:${jobIds.join(",")}`);
  },
  async ApproveRemoteTab(tabId: string, callId: string, decision: string) {
    tape.push(`approve:${tabId}:${callId}:${decision}`);
		if (failApproval) throw new Error("tunnel write failed");
		if (blockApproval) await new Promise<void>((resolve) => { releaseApproval = resolve; });
  },
	async ResolveRemoteTabPlanDecision(tabId: string, callId: string, action: string, feedback: string) {
		tape.push(`plan-decision:${tabId}:${callId}:${action}:${feedback}`);
	},
	async AnswerRemoteTab(tabId: string, callId: string, answers: Array<{ QuestionID: string; Selected: string[] }>) {
		tape.push(`answer:${tabId}:${callId}:${JSON.stringify(answers)}`);
		if (blockAnswer) await new Promise<void>((resolve) => { releaseAnswer = resolve; });
  },
  async SubmitRemoteTabExtensionForm(tabId: string, pluginId: string, surfaceId: string, values: Record<string, unknown>) {
    tape.push(`extension-form:${tabId}:${pluginId}:${surfaceId}:${JSON.stringify(values)}`);
  },
  async OpenRemoteProjectTab(hostId: string, workspace: string, opts?: { newSession?: boolean }) {
    tape.push(`open:${hostId}:${workspace}:${opts?.newSession ? "new" : ""}`);
    if (failOpen) throw new Error("reconnect failed");
    return { ...remoteTab, remote: { hostId, workspace } };
  },
} as Partial<AppBindings> as AppBindings } };

const [{ createRoot }, { RemoteSessionSurface }, { LocaleProvider }, { useRemoteSession }, { __emitMockRemoteTab }, { remoteRuntimeCommand }] = await Promise.all([
  import("react-dom/client"),
  import("../components/RemoteSessionSurface"),
  import("../lib/i18n"),
  import("../lib/useRemoteSession"),
  import("../lib/bridge"),
  import("../lib/useRemoteComposerIntegration"),
]);

const remoteTab: TabMeta = {
  id: "tab-remote-1",
  scope: "project",
  workspaceRoot: "~/app",
  workspaceName: "app",
  topicId: "",
  topicTitle: "app",
  label: "gpu-box",
  ready: true,
  running: false,
  mode: "normal",
  active: true,
  cwd: "~/app",
  remote: { hostId: "gpu-box", workspace: "~/app" },
};

async function flush(ticks = 4) {
  for (let i = 0; i < ticks; i++) await Promise.resolve();
  await new Promise((resolve) => setTimeout(resolve, 40));
}

// The surface takes its session from the hook — the same wiring the app
// shell uses (the shared Transcript renders the content, the composer lives
// in the shell).
function RemoteSurfaceHarness({ tab }: { tab: TabMeta }) {
  const session = useRemoteSession(tab.id);
  return <RemoteSessionSurface tab={tab} session={session} />;
}

// ── Surface: shared Transcript renders reducer-driven items ──
const root = createRoot(document.getElementById("root")!);
await act(async () => {
  root.render(
    <LocaleProvider>
      <RemoteSurfaceHarness tab={remoteTab} />
    </LocaleProvider>,
  );
});
await act(async () => flush());

ok(document.querySelector(".remote-surface__log") === null, "no bespoke log rows — the shared Transcript owns rendering");
ok(!document.querySelector(".remote-surface__composer"), "the surface renders no composer of its own");

await act(async () => {
  __emitMockRemoteTab("tab-remote-1", "event", { kind: "turn_started" });
  __emitMockRemoteTab("tab-remote-1", "event", { kind: "reasoning", reasoning: "thinking hard" });
  __emitMockRemoteTab("tab-remote-1", "event", { kind: "text", text: "streaming answer" });
  await flush();
});
ok(document.body.textContent?.includes("streaming answer") === true, "serve text frames render through the local transcript pipeline");
ok(document.body.textContent?.includes("thinking hard") === true, "reasoning renders through the local pipeline");
{
  const snapshotsBefore = tape.filter((entry) => entry.startsWith("snapshot:")).length;
  await act(async () => {
    __emitMockRemoteTab("tab-remote-1", "state", { state: "ready" });
    await flush();
  });
  ok(tape.filter((entry) => entry.startsWith("snapshot:")).length > snapshotsBefore, "a ready transition re-syncs the snapshot (session reset / reconnect path)");
}
ok(!document.body.textContent?.includes("streaming answer"), "the re-synced snapshot replaces the old session content")

await act(async () => {
  const statusBefore = tape.filter((entry) => entry === "status:tab-remote-1").length;
  __emitMockRemoteTab("tab-remote-1", "event", { kind: "turn_done" });
  await flush();
  ok(tape.filter((entry) => entry === "status:tab-remote-1").length > statusBefore, "turn_done refreshes remote goal/runtime status");
});

await act(async () => {
  __emitMockRemoteTab("tab-remote-1", "event", { kind: "approval_request", approval: { id: "call-9", tool: "bash", subject: "rm -rf /tmp/junk" } });
  await flush();
});
{
  const dialog = document.querySelector(".remote-surface__approval");
  ok(Boolean(dialog), "approval card renders");
  ok(dialog?.textContent?.includes("rm -rf /tmp/junk") === true, "approval subject renders");
  ok(dialog?.textContent?.includes("Allow matching for this session") === true
    && dialog?.textContent?.includes("Always allow matching operations") === true,
  "remote approval exposes session and persistent scopes");
  await act(async () => {
    [...dialog!.querySelectorAll<HTMLButtonElement>(".prompt-action")].find((b) => b.textContent?.includes("Allow matching for this session"))?.click();
    await flush();
  });
  await act(async () => {
    dialog?.querySelector<HTMLButtonElement>(".decision-confirm-bar__confirm")?.click();
    await new Promise((resolve) => setTimeout(resolve, 220));
  });
  ok(tape.includes("approve:tab-remote-1:call-9:session"), "session grant forwards its approval scope");
	ok(!document.querySelector(".remote-surface__approval"), "approval card clears after deciding");
}

await act(async () => {
	__emitMockRemoteTab("tab-remote-1", "event", {
		kind: "approval_request",
		approval: { id: "plan-remote", tool: "exit_plan_mode", subject: "Plan ready" },
	});
	await flush();
});
{
	const planDialog = document.querySelector(".remote-surface__approval");
	await act(async () => {
		[...planDialog!.querySelectorAll<HTMLButtonElement>(".prompt-action")]
			.find((button) => button.textContent?.includes("Revise plan"))?.click();
		await flush();
	});
	await act(async () => {
		const input = planDialog?.querySelector<HTMLTextAreaElement>(".plan-revision__input");
		if (input) {
			Object.getOwnPropertyDescriptor(dom.window.HTMLTextAreaElement.prototype, "value")?.set?.call(input, "cover the rollback path");
			input.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
		}
		await flush();
	});
	await act(async () => {
		planDialog?.querySelector<HTMLButtonElement>(".plan-revision__actions .btn--primary")?.click();
		await new Promise((resolve) => setTimeout(resolve, 220));
	});
	ok(tape.includes("plan-decision:tab-remote-1:plan-remote:revise_plan:cover the rollback path"),
		"remote plan revision uses the specialized decision endpoint and preserves feedback");
	ok(!tape.some((entry) => entry.startsWith("approve:tab-remote-1:plan-remote")),
		"remote plan decisions never collapse into generic approval booleans");
}

await act(async () => {
	failApproval = true;
	__emitMockRemoteTab("tab-remote-1", "event", { kind: "approval_request", approval: { id: "call-fail", tool: "bash", subject: "keep this prompt" } });
	await flush();
});
{
	await act(async () => {
		const failedDialog = document.querySelector(".remote-surface__approval");
		[...failedDialog!.querySelectorAll<HTMLButtonElement>(".prompt-action")].find((b) => b.textContent?.includes("Allow once"))?.click();
		await flush();
	});
	await act(async () => {
		const failedDialog = document.querySelector(".remote-surface__approval");
		failedDialog?.querySelector<HTMLButtonElement>(".decision-confirm-bar__confirm")?.click();
		await new Promise((resolve) => setTimeout(resolve, 220));
		await flush();
	});
	ok(Boolean(document.querySelector(".remote-surface__approval")), "a failed approval command preserves the decision card");
	ok(document.body.textContent?.includes("tunnel write failed") === true, "a failed approval command surfaces an actionable error");
	failApproval = false;
}

await act(async () => {
  __emitMockRemoteTab("tab-remote-1", "event", { kind: "ask_request", ask: { id: "ask-7", questions: [{ id: "q1", prompt: "Deploy now?", options: [{ label: "yes" }, { label: "no" }] }] } });
  await flush();
});
{
  const dialog = document.querySelector(".prompt-shelf--ask");
  ok(Boolean(dialog), "ask card renders");
  ok(dialog?.textContent?.includes("Deploy now?") === true, "ask prompt renders");
	await act(async () => {
		[...document.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent?.trim() === "yes")?.click();
		await flush();
	});
	ok(!tape.some((entry) => entry.startsWith("answer:tab-remote-1:ask-7")), "selecting an option keeps the ask open until explicit submit");
	await act(async () => {
		[...document.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent?.trim() === "Submit")?.click();
		await flush();
	});
	ok(tape.includes('answer:tab-remote-1:ask-7:[{"QuestionID":"q1","Selected":["yes"]}]'), "submit forwards the question id and complete selection batch");
}

await act(async () => {
  __emitMockRemoteTab("tab-remote-1", "event", { kind: "ask_request", ask: { id: "ask-custom", questions: [{ id: "q-custom", prompt: "Where?", options: [{ label: "staging" }] }] } });
  await flush();
});
await act(async () => {
  [...document.querySelectorAll<HTMLButtonElement>("button")].find((button) => button.textContent?.trim() === "Other answer")?.click();
  await flush();
});
await act(async () => {
  const input = document.querySelector<HTMLInputElement>(".ask-shelf__custom");
  if (input) {
    Object.getOwnPropertyDescriptor(dom.window.HTMLInputElement.prototype, "value")?.set?.call(input, "canary");
    input.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
  }
  await flush();
});
await act(async () => {
  [...document.querySelectorAll<HTMLButtonElement>("button")].find((button) => button.textContent?.trim() === "Submit")?.click();
  await flush();
});
ok(tape.includes('answer:tab-remote-1:ask-custom:[{"QuestionID":"q-custom","Selected":["canary"]}]'),
  "custom AskCard text serializes as the remote question selection");

await act(async () => {
  __emitMockRemoteTab("tab-remote-1", "event", {
    kind: "extension_surface",
    extension: {
      pluginId: "remote-plugin", surfaceId: "setup", kind: "form",
      form: { title: "Remote setup", fields: [{ key: "region", label: "Region", kind: "input", required: true, default: "us-west" }] },
    },
  });
  await flush();
});
{
  const form = document.querySelector(".extension-form");
  ok(Boolean(form) && document.body.textContent?.includes("Remote setup") === true, "remote extension form renders on the shared surface");
  await act(async () => {
    form?.closest(".prompt-shelf")?.querySelector<HTMLButtonElement>(".decision-confirm-bar__confirm")?.click();
    await flush();
  });
  ok(tape.includes('extension-form:tab-remote-1:remote-plugin:setup:{"region":"us-west"}'),
    "remote extension form submits through the Serve proxy");
  ok(!document.querySelector(".extension-form"), "remote extension form clears after an accepted submission");
}

await act(async () => {
  __emitMockRemoteTab("tab-remote-1", "state", { state: "serve_down", error: "tunnel closed" });
  await flush();
});
{
  const warning = document.querySelector(".remote-surface--warning");
  ok(Boolean(warning), "serve_down renders the warning state");
  ok(warning?.textContent?.includes("tunnel closed") === true, "serve error detail renders");
  await act(async () => {
    warning?.querySelector<HTMLButtonElement>("button")?.click();
    await flush();
  });
  ok(tape.includes("open:gpu-box:~/app:"), "serve_down retry preserves the backend's parked session target");
  failOpen = true;
  await act(async () => {
    warning?.querySelector<HTMLButtonElement>("button")?.click();
    await flush();
  });
  ok(warning?.textContent?.includes("reconnect failed") === true, "serve_down retry failures render on the surface");
  failOpen = false;
}

// ── Restored shell: the disconnected state renders a reconnect affordance
// whose click reopens the project (fresh blank session) ──
await act(async () => {
  __emitMockRemoteTab("tab-remote-1", "state", { state: "disconnected" });
  await flush();
});
{
  const shell = document.querySelector(".remote-surface--disconnected");
  ok(Boolean(shell), "disconnected renders the restored-shell state");
  const reconnect = [...document.querySelectorAll<HTMLButtonElement>("button")].find((b) => b.textContent?.includes("Reconnect"));
  ok(Boolean(reconnect), "the disconnected surface offers a reconnect button");
  await act(async () => {
    reconnect?.click();
    await flush();
  });
  ok(tape.includes("open:gpu-box:~/app:new"), "reconnect reopens the project with a fresh session");
}

await act(async () => root.unmount());

// ── Hook: optimistic user bubble + command forwarding ──
let probe: RemoteSessionApi | undefined;
function HookProbe({ tabId = "tab-remote-2" }: { tabId?: string }) {
  probe = useRemoteSession(tabId);
  return null;
}
const probeRoot = createRoot(document.createElement("div"));
await act(async () => { probeRoot.render(<LocaleProvider><HookProbe /></LocaleProvider>); await flush(); });
ok(probe?.state === "ready", "a successful fenced snapshot recovers a ready event missed before listener mount");
ok(probe?.composerProfile?.collaborationMode === "plan" && probe?.composerProfile?.toolApprovalMode === "auto" && probe.goalRuntime?.tokensUsed === 321,
  "snapshot status hydrates the authoritative remote composer profile");
ok(probe?.effort?.current === "high" && probe?.transcript.checkpoints[0]?.turn === 3
  && probe?.transcript.checkpoints[0]?.fileCount === 2 && probe?.transcript.checkpoints[0]?.files.length === 1,
  "snapshot status hydrates effort and rewind checkpoints");
ok(probe?.commands.length === 1 && probe.commands[0]?.name === "remote-review",
  "remote hydration exposes the Serve command catalog instead of local commands");
ok(probe?.transcript.context.used === 1200 && probe.transcript.context.window === 64000
  && probe.transcript.balance?.display === "¥88.00" && probe.transcript.sessionCost === 0.12
  && probe.transcript.jobs[0]?.id === "job-remote" && probe.transcript.lastTurnOutputTokens === 200,
  "snapshot status hydrates remote context, usage, balance, cost, and jobs");
ok(remoteRuntimeCommand("/goal write regression tests") === undefined && remoteRuntimeCommand("/goal --strict write regression tests") === undefined, "goal-setting commands remain conversational turns");
ok(remoteRuntimeCommand("/goal")?.method === "runManagementCommand" && remoteRuntimeCommand("/goal status")?.method === "runManagementCommand"
  && remoteRuntimeCommand("/goal --strict pause")?.method === "runManagementCommand",
  "goal status and lifecycle actions remain synchronous management commands");
ok(remoteRuntimeCommand("/branch experiment")?.rehydrate === true && remoteRuntimeCommand("/switch main")?.rehydrate === true
  && remoteRuntimeCommand("/rewind 3 conversation")?.rehydrate === true && remoteRuntimeCommand("/context")?.rehydrate !== true
  && remoteRuntimeCommand("/compact preserve tests")?.method === "compact",
  "session-changing management commands request authoritative rehydration");
statusPendingPrompt = true; replayedPrompts = [{ kind: "approval_request", approval: { id: "recovered-prompt", tool: "bash", subject: "replayed after drop" } }];
await act(async () => { await probe?.runManagementCommand("/context"); await flush(); });
ok(tape.includes("replay-prompts:tab-remote-2") && probe?.transcript.approval?.id === "recovered-prompt", "pending status recovers an SSE-dropped prompt through Serve");
await act(async () => { await probe?.approve("recovered-prompt", "deny"); await flush(); }); statusPendingPrompt = false; replayedPrompts = [];
let remoteLiveNotifications = 0;
const unsubscribeRemoteLive = probe?.liveStore.subscribe("tab-remote-2", () => { remoteLiveNotifications += 1; });
await act(async () => {
	__emitMockRemoteTab("tab-remote-2", "event", { kind: "turn_started", turnStartedAt: 1234 });
	__emitMockRemoteTab("tab-remote-2", "event", { kind: "text", text: "remote live ticker" });
	await flush();
});
ok(probe?.liveStore.getSnapshot("tab-remote-2")?.text === "remote live ticker" && remoteLiveNotifications > 0,
	"remote live-store notifications drive the shared composer ticker");
unsubscribeRemoteLive?.();
const turnDoneGeneration = probe?.surfaceGeneration;
statusGoalStatus = "complete";
snapshotHistory = [{ role: "user", content: "server-side prompt" }, { role: "assistant", content: "reconciled final answer" }];
await act(async () => {
  __emitMockRemoteTab("tab-remote-2", "event", { kind: "turn_done" });
  await flush();
});
ok(probe?.composerProfile?.goalStatus === "complete" && probe.surfaceGeneration === turnDoneGeneration,
  "turn_done refreshes goal status without replacing the transcript surface");
ok(probe?.transcript.items.some((item) => item.kind === "assistant" && item.text === "reconciled final answer") === true,
  "turn_done reconciles durable history after dropped Serve frames");

await act(async () => {
  __emitMockRemoteTab("tab-remote-2", "event", { kind: "approval_request", approval: { id: "approval-old", tool: "bash", subject: "old" } });
  await flush();
});
blockApproval = true;
let oldApproval: Promise<void> | undefined;
await act(async () => {
  oldApproval = probe?.approve("approval-old", "allow");
  await flush();
  __emitMockRemoteTab("tab-remote-2", "event", { kind: "approval_request", approval: { id: "approval-next", tool: "bash", subject: "next" } });
  releaseApproval?.();
  await oldApproval;
  await flush();
});
blockApproval = false;
ok(probe?.transcript.approval?.id === "approval-next", "an answered approval cannot clear the next prompt");

await act(async () => {
  __emitMockRemoteTab("tab-remote-2", "event", { kind: "ask_request", ask: { id: "ask-old", questions: [] } });
  await flush();
});
blockAnswer = true;
let oldAnswer: Promise<void> | undefined;
await act(async () => {
  oldAnswer = probe?.answer("ask-old", []);
  await flush();
  __emitMockRemoteTab("tab-remote-2", "event", { kind: "ask_request", ask: { id: "ask-next", questions: [] } });
  releaseAnswer?.();
  await oldAnswer;
  await flush();
});
blockAnswer = false;
ok(probe?.transcript.ask?.id === "ask-next", "an answered ask cannot clear the next prompt");
await act(async () => { await probe?.submit("run tests"); await flush(); });
ok(Boolean(probe?.transcript.items.some((item) => item.kind === "user" && item.text === "run tests")), "submit adds the optimistic user bubble through the shared reducer");
await act(async () => { await probe?.runManagementCommand("/context"); await flush(); });
ok(tape.includes("submit:tab-remote-2:/context") && tape.includes("status:tab-remote-2"),
  "management commands dispatch without conversational admission and refresh status");
ok(!probe?.transcript.items.some((item) => item.kind === "user" && item.text === "/context"),
  "management commands do not add an optimistic user turn");
const beforeCompactGeneration = probe?.surfaceGeneration;
snapshotHistory = [{ role: "assistant", content: "compacted remote history" }];
await act(async () => { await probe?.compact("preserve tests"); await flush(); });
ok(tape.includes("compact:tab-remote-2:preserve tests") && probe?.surfaceGeneration === (beforeCompactGeneration ?? 0) + 1 && probe.transcript.items.some((item) => item.kind === "assistant" && item.text === "compacted remote history"), "remote compact waits for the dedicated endpoint and rehydrates history");
const beforeSwitchGeneration = probe?.surfaceGeneration;
snapshotHistory = [{ role: "assistant", content: "adopted switched session" }];
await act(async () => { await probe?.runManagementCommand("/switch feature", true); await flush(); });
ok(tape.includes("submit:tab-remote-2:/switch feature") && probe?.surfaceGeneration === (beforeSwitchGeneration ?? 0) + 1
  && probe.transcript.items.some((item) => item.kind === "assistant" && item.text === "adopted switched session"),
  "session-changing management commands replace history from an authoritative snapshot");
ok(!probe?.transcript.items.some((item) => item.kind === "user" && item.text === "/switch feature"),
  "session-changing management commands still avoid an optimistic user turn");
await act(async () => {
  await probe?.cancelTurn();
  await probe?.approve("call-1", "allow");
  await probe?.answer("ask-1", [{ QuestionID: "q1", Selected: ["yes"] }]);
  await probe?.rewind(3, "code");
  await probe?.rewind(3, "fork");
  await probe?.rewind(3, "summ-from");
  await probe?.rewind(3, "summ-upto");
  await flush();
});
const metadataGeneration = probe?.surfaceGeneration;
statusGoalStatus = "complete";
await act(async () => {
  await probe?.setModel("remote/new-model");
  await probe?.setEffort("high");
  await probe?.setQualityFloor("delivery");
  await probe?.pauseGoal();
  await probe?.resumeGoal();
  await probe?.steer("narrow the change");
  await probe?.cancelJob("job-remote");
  await flush();
});
ok(probe?.surfaceGeneration === metadataGeneration, "metadata-only remote commands preserve the transcript generation and viewport");
ok(probe?.composerProfile?.goalStatus === "complete" && probe.composerProfile.qualityFloor === "delivery",
  "status-only refresh updates goal status and quality floor");
ok(probe?.modelLabel === "Model · remote/new-model" && probe.effort?.current === "high",
  "model switching refreshes the authoritative remote profile before the next turn");
for (const want of [
  "submit:tab-remote-2:run tests",
  "cancel:tab-remote-2",
  "approve:tab-remote-2:call-1:allow",
	'answer:tab-remote-2:ask-1:[{"QuestionID":"q1","Selected":["yes"]}]',
  "rewind:tab-remote-2:3:code",
  "fork:tab-remote-2:3:",
  "summarize:tab-remote-2:3:from",
  "summarize:tab-remote-2:3:upto",
  "model:tab-remote-2:remote/new-model",
  "effort:tab-remote-2:high",
  "quality-floor:tab-remote-2:delivery",
  "pause-goal:tab-remote-2",
  "resume-goal:tab-remote-2",
  "steer:tab-remote-2:narrow the change",
	"cancel-jobs:tab-remote-2:job-remote",
]) {
  ok(tape.includes(want), `command forwarded: ${want}`);
}

await act(async () => {
  probeRoot.render(<LocaleProvider><HookProbe tabId="tab-pending-model" /></LocaleProvider>);
  await Promise.resolve();
});
ok(probe?.modelLabel === "", "switching remote tabs clears the previous model label before hydration");

await act(async () => probeRoot.unmount());

let fallbackProbe: RemoteSessionApi | undefined;
function FallbackProbe() {
  fallbackProbe = useRemoteSession("tab-status-fallback");
  return null;
}
const fallbackRoot = createRoot(document.createElement("div"));
await act(async () => {
  fallbackRoot.render(<LocaleProvider><FallbackProbe /></LocaleProvider>);
  await flush();
});
ok(fallbackProbe?.hydrated === true && fallbackProbe.composerProfile?.collaborationMode === "plan"
  && fallbackProbe.composerProfile.toolApprovalMode === "yolo"
  && tape.includes("status:tab-status-fallback"),
  "missing aggregate status is fetched before the remote composer becomes ready");
await act(async () => fallbackRoot.unmount());

let toolProbe: RemoteSessionApi | undefined;
function ToolProbe() {
  toolProbe = useRemoteSession("tab-tool-history");
  return null;
}
const toolRoot = createRoot(document.createElement("div"));
await act(async () => {
  toolRoot.render(<LocaleProvider><ToolProbe /></LocaleProvider>);
  await flush();
});
const remoteTool = toolProbe?.transcript.items.find((item) => item.kind === "tool" && item.id === "remote-tool");
ok(remoteTool?.kind === "tool" && remoteTool.args.includes("go test ./...")
  && remoteTool.output === "remote tool output" && remoteTool.dataArchived !== true,
  "remote history retains expandable tool args and output without a local rehydrate endpoint");
await act(async () => toolRoot.unmount());

let failureProbe: RemoteSessionApi | undefined;
function FailureProbe() {
  failureProbe = useRemoteSession("tab-hydration-failure", "ready");
  return null;
}
const failureRoot = createRoot(document.createElement("div"));
await act(async () => {
  failureRoot.render(<LocaleProvider><FailureProbe /></LocaleProvider>);
});
await act(async () => {
  await new Promise((resolve) => setTimeout(resolve, 2200));
});
ok(failureProbe?.hydrated === false && failureProbe.error.includes("history exceeds bridge limit"),
  "exhausted ready-session hydration exposes a retryable error");
failHydration = false;
await act(async () => {
  await failureProbe?.retryHydration();
  await flush();
});
ok(failureProbe?.hydrated === true && failureProbe.error === "", "explicit hydration retry recovers the surface");
await act(async () => failureRoot.unmount());

// ── Hydration fence: an SSE event delivered while the snapshot is in flight
// is replayed after history instead of being overwritten by it. ──
let raceProbe: RemoteSessionApi | undefined;
function RaceProbe() {
	raceProbe = useRemoteSession("tab-race");
	return null;
}
const raceRoot = createRoot(document.createElement("div"));
await act(async () => {
	raceRoot.render(<LocaleProvider><RaceProbe /></LocaleProvider>);
	await Promise.resolve();
});
await act(async () => {
	__emitMockRemoteTab("tab-race", "event", { kind: "turn_started" });
	__emitMockRemoteTab("tab-race", "event", { kind: "text", text: "arrived during hydration" });
	resolveRaceSnapshot?.({
		history: [],
		status: { running: true, label: "Race", plan: false, toolApprovalMode: "ask", goal: "" },
	});
	await flush();
});
ok(raceProbe?.transcript.live.text === "arrived during hydration", "hydration replays concurrently delivered remote events");
await act(async () => raceRoot.unmount());

let stateRaceProbe: RemoteSessionApi | undefined;
function StateRaceProbe() {
	stateRaceProbe = useRemoteSession("tab-state-race");
	return null;
}
const stateRaceRoot = createRoot(document.createElement("div"));
await act(async () => {
	stateRaceRoot.render(<LocaleProvider><StateRaceProbe /></LocaleProvider>);
	await Promise.resolve();
});
await act(async () => {
	__emitMockRemoteTab("tab-state-race", "state", { state: "reconnecting" });
	__emitMockRemoteTab("tab-state-race", "state", { state: "ready" });
	resolveStateRaceSnapshots[0]?.({
		history: [],
		status: { running: false, label: "Stale", plan: false, toolApprovalMode: "ask", goal: "" },
	});
	await flush();
});
await act(async () => {
	resolveStateRaceSnapshots[1]?.({
		history: [{ role: "assistant", content: "fresh generation" }],
		status: { running: false, label: "Fresh", plan: false, toolApprovalMode: "ask", goal: "" },
	});
	await flush();
});
ok(stateRaceProbe?.state === "ready" && stateRaceProbe.hydrated === true
	&& stateRaceProbe.modelLabel === "Fresh"
	&& stateRaceProbe.transcript.items.some((item) => item.kind === "assistant" && item.text === "fresh generation"),
	"a ready generation re-hydrates after discarding the stale in-flight snapshot");
await act(async () => stateRaceRoot.unmount());

// Post-turn reconciliation can overlap a ready-to-ready /new, /clear, or
// resume. The old history response must not replace the newly adopted session.
let rotationProbe: RemoteSessionApi | undefined;
function RotationProbe() { rotationProbe = useRemoteSession("tab-reconcile-rotation"); return null; }
const rotationRoot = createRoot(document.createElement("div"));
await act(async () => { rotationRoot.render(<LocaleProvider><RotationProbe /></LocaleProvider>); await flush(); });
await act(async () => {
	__emitMockRemoteTab("tab-reconcile-rotation", "event", { kind: "turn_started" });
	__emitMockRemoteTab("tab-reconcile-rotation", "event", { kind: "turn_done" });
	await Promise.resolve();
	__emitMockRemoteTab("tab-reconcile-rotation", "state", { state: "ready" });
	await flush();
});
ok(rotationProbe?.transcript.items.some((item) => item.kind === "assistant" && item.text === "fresh rotated session") === true,
	"ready-to-ready rotation hydrates the adopted session while old reconciliation is pending");
await act(async () => { __emitMockRemoteTab("tab-reconcile-rotation", "event", { kind: "turn_started" }); __emitMockRemoteTab("tab-reconcile-rotation", "event", { kind: "turn_done" }); await Promise.resolve(); });
await act(async () => {
	resolveRotationReconcile?.({
		history: [{ role: "assistant", content: "stale previous session" }],
		status: { running: false, label: "Stale", plan: false, toolApprovalMode: "ask", goal: "" },
	});
	await flush();
});
ok(rotationProbe?.transcript.items.some((item) => item.kind === "assistant" && item.text === "fresh reconciled turn") === true
	&& !rotationProbe.transcript.items.some((item) => item.kind === "assistant" && item.text === "stale previous session"),
	"session generation fence rejects stale history and hands reconciliation to the new generation");
await act(async () => rotationRoot.unmount());

// Pending prompt frames retained by Desktop are replayed by the next snapshot,
// which restores decisions missed while the tab had no frontend listener.
let replayProbe: RemoteSessionApi | undefined;
function ReplayProbe() { replayProbe = useRemoteSession("tab-replay"); return null; }
const replayRoot = createRoot(document.createElement("div"));
await act(async () => {
	replayRoot.render(<LocaleProvider><ReplayProbe /></LocaleProvider>);
	await flush();
});
ok(replayProbe?.transcript.approval?.id === "replayed-approval", "snapshot replays a prompt emitted while the remote tab was inactive");
ok(replayProbe?.transcript.extensionForm?.pluginId === "replayed-plugin" && replayProbe.transcript.extensionForm.surfaceId === "replayed-form", "snapshot replays an extension form emitted while the remote tab was inactive");
await act(async () => { replayProbe?.drainApprovals(["different-approval"]); await flush(); });
ok(replayProbe?.transcript.approval?.id === "replayed-approval", "a remote mode transaction preserves approvals it did not drain");
await act(async () => { replayProbe?.drainApprovals(["replayed-approval"]); await flush(); });
ok(replayProbe?.transcript.approval === undefined, "a remote mode transaction clears the exact approval it auto-allowed");
await act(async () => replayRoot.unmount());
dom.window.close();
process.stdout.write(`\n${passed} passed, ${failed} failed\n`);
if (failed > 0) process.exit(1);
