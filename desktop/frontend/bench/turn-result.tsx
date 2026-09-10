import React, { useState } from "react";
import { createRoot } from "react-dom/client";
import { Transcript } from "../src/components/Transcript";
import { WorkspaceTurnResult } from "../src/components/WorkspaceTurnResult";
import { initialState, reducer, type Item } from "../src/lib/useController";
import { historicalResultNotice } from "../src/lib/completionResultState";
import { app, type AppBindings } from "../src/lib/bridge";
import { installDesktopHostStub } from "../src/__tests__/desktopHostStub";
import { LocaleProvider, useI18n } from "../src/lib/i18n";
import type { TurnChanges, WireCompletionSummary, WireEvent } from "../src/lib/types";
import "../src/styles.css";

const path = "desktop/frontend/src/components/deeply/nested/very-long-component-name/WorkspaceTurnResult.tsx";
const diff: TurnChanges = { id: "0:42", turn: 0, coverage: "complete", added: 18, removed: 6, reasons: [], files: [
  { path, kind: "modify", added: 15, removed: 6 },
  { path: "internal/checkpoint/turn_changes.go", kind: "create", added: 3, removed: 0 },
] };
let unavailable = false;
const calls: string[] = [];
const fallback = Object.fromEntries(["ReportFrontendDiagnostic", "ToolResultForTab", "ListDirForTab", "WorkspaceChanges"].map(k => [k, app[k as keyof AppBindings]]));
installDesktopHostStub(new Proxy({
  ...fallback,
  WorkspaceTurnChanges: async (_tab: string, _session: string, _turn: number, id: string) => { calls.push("summary:" + id); return unavailable ? { ...diff, id: undefined, coverage: "unknown", files: [] } : { ...diff, id }; },
  WorkspaceTurnChangeDetail: async (_tab: string, _session: string, _turn: number, id: string, file: string) => { calls.push("detail:" + id); return unavailable ? null : { ...diff.files.find(f => f.path === file), patch: "@@ -1,2 +1,3 @@\n-old result\n+frozen result for " + id + "\n+confirmed changes\n context" }; },
  TurnCheckLog: async (_tab: string, session: string, id: string) => { calls.push("log:" + session + ":" + id); return unavailable ? null : { output: "go test ./...\n--- FAIL: TestFrozenResult\nexpected 0, got 1\nFAIL\nexit status 1", truncated: false }; },
}, { get(target, key) { return target[key as keyof typeof target] ?? (async () => undefined); } }) as unknown as AppBindings);
(window as unknown as { turnResultCalls: string[] }).turnResultCalls = calls;

function snapshot(scenario: string) {
  const event = (s: typeof initialState, e: WireEvent) => reducer(s, { type: "event", e });
  let state = reducer(initialState, { type: "user", text: "完善本轮结果展示，保留历史差异和检查日志。" });
  state = event(state, { kind: "turn_started", turnId: "fixture-turn", checkpointTurn: 0 });
  state = event(state, { kind: "tool_dispatch", turnId: "fixture-turn", tool: { id: "check-1", name: "exec_command", args: '{"command":"go test ./..."}', readOnly: false } });
  state = event(state, { kind: "tool_progress", turnId: "fixture-turn", tool: { id: "check-1", name: "exec_command", verifying: true, output: "running checkpoint tests..." } });
  if (scenario === "running") return state;
  const failed = scenario === "failed";
  const checks = scenario === "none" ? [] : [{ command: "go test ./...", passed: !failed && scenario !== "interrupted", stale: scenario === "stale", interrupted: scenario === "interrupted", exitCode: failed ? 1 : 0, toolCallId: "check-1", toolResultId: "log-entry-1" }];
  const receipt = { verdict: "partial", interrupted: scenario === "interrupted", diff: scenario === "legacy" ? undefined : { ...diff, coverage: scenario === "partial" ? "partial" as const : "complete" as const, reasons: scenario === "partial" ? ["external_change"] : [] }, verifications: checks };
  state = event(state, { kind: "tool_result", turnId: "fixture-turn", tool: { id: "check-1", name: "exec_command", output: "check completed" } });
  state = event(state, { kind: "message", text: "已补齐本轮差异与检查记录。文件统计只计算已确认的净变更。", turnId: "fixture-turn" });
  state = event(state, { kind: "completion_summary", turnId: "fixture-turn", completion: { preset: "balanced", verdict: "partial", mutations: 30, checks_passed: checks.length && !failed ? 1 : 0, checks_failed: failed ? 1 : 0, checks_suppressed: 0, review: "none", attention: failed } });
  state = event(state, { kind: "turn_done", turnId: "fixture-turn", checkpointTurn: 0, receipt: scenario === "legacy" ? undefined : receipt });
  if (scenario === "history") {
    const saved = historicalResultNotice({ role: "notice", content: "", completionReceipt: { ...receipt, diff: { ...diff, id: "0:historical" } }, checkpointTurn: 0, turnId: "historical" }, "old-result")!;
    state.items = [{ kind: "user", id: "old-user", text: "这是历史轮次" }, saved, { kind: "assistant", id: "old-answer", text: "历史修改已完成。", reasoning: "", streaming: false }, { kind: "user", id: "middle-user", text: "继续说明" }, { kind: "assistant", id: "middle-answer", text: "这一轮没有文件修改。", reasoning: "", streaming: false }, ...state.items] as Item[];
  }
  return state;
}

function Fixture() {
  const locale = useI18n();
  const [scenario, setScenario] = useState("failed");
  const [dark, setDark] = useState(true);
  const [width, setWidth] = useState(410);
  const [selection, setSelection] = useState<{ summary: WireCompletionSummary; view: "changes" | "checks"; key: number }>();
  const [missing, setMissing] = useState(false);
  const state = React.useMemo(() => snapshot(scenario), [scenario]);
  React.useEffect(() => { document.documentElement.dataset.theme = dark ? "dark" : "light"; document.documentElement.dataset.themeStyle = "graphite"; }, [dark]);
  React.useEffect(() => { locale.setPref("zh"); }, [locale.setPref]);
  const show = (summary: WireCompletionSummary | undefined, view: "changes" | "checks") => summary && setSelection({ summary, view, key: Date.now() });
  return <div style={{ height: "100vh", display: "grid", gridTemplateRows: "auto 1fr", background: "var(--bg)", color: "var(--fg)" }}>
    <nav style={{ display: "flex", gap: 8, padding: 12, flexWrap: "wrap", borderBottom: "1px solid var(--border-soft)" }} aria-label="Browser fixtures">
      <label>场景 <select aria-label="Scenario" value={scenario} onChange={e => { setScenario(e.target.value); setSelection(undefined); }}>{["failed","passed","none","stale","interrupted","running","partial","legacy","history"].map(s => <option key={s}>{s}</option>)}</select></label>
      <button onClick={() => setDark(v => !v)}>深浅色</button>
      <button onClick={() => setWidth(v => v === 410 ? 280 : 410)}>面板宽度 {width}</button>
      <button aria-pressed={missing} onClick={() => { unavailable = !missing; setMissing(!missing); setSelection(undefined); }}>模拟数据已清理</button>
    </nav>
    <main style={{ minHeight: 0, display: "grid", gridTemplateColumns: selection ? "minmax(0,1fr) " + width + "px" : "minmax(0,1fr)" }}>
      <div style={{ minWidth: 0, minHeight: 0, display: "flex", flexDirection: "column" }}><Transcript items={state.items} running={state.running} turnStartAt={state.turnStartAt} tabId="fixture" geometrySessionKey="fixture" onPrompt={() => {}} onOpenChanges={s => show(s, "changes")} onOpenVerification={s => show(s, "checks")} /></div>
      {selection && <aside style={{ overflow: "auto", minWidth: 0, borderLeft: "1px solid var(--border-soft)" }}><WorkspaceTurnResult key={selection.key} summary={selection.summary} tabId="fixture" sessionPath="/fixture-session.json" initialView={selection.view} onAllChanges={() => setSelection(undefined)} /></aside>}
    </main>
  </div>;
}
createRoot(document.getElementById("root")!).render(<LocaleProvider><Fixture /></LocaleProvider>);
