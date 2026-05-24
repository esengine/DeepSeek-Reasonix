import { normalizeMcpConfig, readConfig } from "../../config.js";
import { t } from "../../i18n/index.js";
import type { CacheFirstLoop } from "../../loop.js";
import { McpClient } from "../../mcp/client.js";
import { type InspectionReport, inspectMcpServer } from "../../mcp/inspect.js";
import { preflightStdioSpec } from "../../mcp/preflight.js";
import { type McpClientHost, bridgeMcpTools } from "../../mcp/registry.js";
import { overlayMatchedSpec, parseMcpSpec, specToRaw } from "../../mcp/spec.js";
import { buildMcpServerSummary } from "../../mcp/summary.js";
import { buildTransportFromSpec } from "../../mcp/transport-from-spec.js";
import type { ToolRegistry } from "../../tools.js";
import type { ToolSpec } from "../../types.js";
import { type McpLifecycleEvent, formatMcpLifecycleEvent } from "../ui/mcp-lifecycle.js";
import { formatMcpSlowToast } from "../ui/mcp-toast.js";
import type { McpServerSummary } from "../ui/slash.js";

export interface ProgressInfo {
  toolName: string;
  progress: number;
  total?: number;
  message?: string;
}

interface SpecRecord {
  spec: string;
  client: McpClient;
  summary: McpServerSummary;
  /** Names of bridged tools — used for hot-unbridge. */
  registeredNames: string[];
  /** ToolSpec snapshots captured AFTER bridge — handed to loop.prefix.addTool on hot-add. */
  registeredSpecs: ToolSpec[];
}

export interface RuntimeContext {
  getTools: () => ToolRegistry | undefined;
  getMcpPrefix: () => string | undefined;
  getRequestedCount: () => number;
  getWorkspaceDir?: () => string | undefined;
  progressSink: { current: ((info: ProgressInfo) => void) | null };
}

export type McpLifecycleNotice =
  | { kind: "handshake"; name: string }
  | {
      kind: "connected";
      name: string;
      tools: number;
      resources: number;
      prompts: number;
      ms: number;
    }
  | { kind: "disabled"; name: string }
  | { kind: "failed"; name: string; reason: string }
  | { kind: "slow"; serverName: string; p95Ms: number; sampleSize: number }
  | { kind: "tools-ready"; name: string; tools: number; ms: number }
  | { kind: "warn"; name: string; reason: string };

export type McpLifecycleSink = (notice: McpLifecycleNotice) => void;

export const stderrLifecycleSink: McpLifecycleSink = (n) => {
  if (n.kind === "slow") {
    process.stderr.write(
      `${formatMcpSlowToast({ name: n.serverName, p95Ms: n.p95Ms, sampleSize: n.sampleSize })}\n`,
    );
    return;
  }
  if (n.kind === "failed") {
    process.stderr.write(
      `${formatMcpLifecycleEvent({ state: "failed", name: n.name, reason: n.reason })}\n  → ${t("mcpLifecycle.failedSetupHint")}\n`,
    );
    return;
  }
  if (n.kind === "connected") {
    process.stderr.write(
      `${formatMcpLifecycleEvent({
        state: "connected",
        name: n.name,
        tools: n.tools,
        resources: n.resources,
        prompts: n.prompts,
        ms: n.ms,
      })}\n`,
    );
    return;
  }
  if (n.kind === "tools-ready") {
    process.stderr.write(
      `${formatMcpLifecycleEvent({ state: "tools-ready", name: n.name, tools: n.tools, ms: n.ms })}\n`,
    );
    return;
  }
  if (n.kind === "warn") {
    process.stderr.write(
      `${formatMcpLifecycleEvent({ state: "warn", name: n.name, reason: n.reason })}\n`,
    );
    return;
  }
  // handshake / disabled — no extra fields needed
  process.stderr.write(
    `${formatMcpLifecycleEvent({ state: n.kind as "handshake" | "disabled", name: n.name })}\n`,
  );
};

export interface McpFailure {
  spec: string;
  name: string;
  reason: string;
  at: number;
}

export interface McpRuntime {
  size(): number;
  specs(): string[];
  summaries(): McpServerSummary[];
  /** Last bridge failure per spec — drives the "未桥接" reason shown in the dashboard. */
  failures(): McpFailure[];
  addSpec(
    raw: string,
    loop?: CacheFirstLoop,
    signal?: AbortSignal,
  ): Promise<{ ok: true; summary: McpServerSummary } | { ok: false; reason: string }>;
  removeSpec(raw: string, loop?: CacheFirstLoop): Promise<boolean>;
  reloadFromConfig(loop?: CacheFirstLoop): Promise<{
    added: string[];
    removed: string[];
    failed: Array<{ spec: string; reason: string }>;
    summaries: McpServerSummary[];
  }>;
  closeAll(): Promise<void>;
  /** Replace the sink that lifecycle events flow through — App.tsx swaps this in on mount so toasts land in the alt-screen UI instead of corrupting it via stderr. */
  setLifecycleSink(sink: McpLifecycleSink): void;
}

export function createMcpRuntime(ctx: RuntimeContext): McpRuntime {
  const records = new Map<string, SpecRecord>();
  const insertionOrder: string[] = [];
  const failureMap = new Map<string, McpFailure>();
  let sink: McpLifecycleSink = stderrLifecycleSink;

  async function addSpec(
    raw: string,
    loop?: CacheFirstLoop,
    signal?: AbortSignal,
  ): Promise<{ ok: true; summary: McpServerSummary } | { ok: false; reason: string }> {
    if (records.has(raw)) {
      return { ok: true, summary: records.get(raw)!.summary };
    }
    failureMap.delete(raw);
    const tools = ctx.getTools();
    if (!tools) return { ok: false, reason: "no tool registry available" };
    const cfg = readConfig();
    const normalized = normalizeMcpConfig(cfg);
    let label = "anon";
    let mcp: McpClient | undefined;
    // Per-server readiness gate — tool dispatches via the bridge await
    // this before calling into `live.callTool`. Resolved on `connected`,
    // rejected on `failed`, so a tool invoked mid-handshake waits
    // (capped by `bridgeMcpTools`'s `readyTimeoutMs`) instead of
    // surfacing a transport error.
    let resolveReady!: () => void;
    let rejectReady!: (err: Error) => void;
    const ready = new Promise<void>((resolve, reject) => {
      resolveReady = resolve;
      rejectReady = reject;
    });
    // Avoid unhandledRejection if no consumer awaits `ready` yet.
    ready.catch(() => undefined);
    try {
      const parsed = parseMcpSpec(raw);
      label = parsed.name ?? "anon";
      const matched = parsed.name ? normalized.find((s) => s.name === parsed.name) : undefined;
      const spec = overlayMatchedSpec(parsed, matched);
      if (spec.disabled) {
        sink({ kind: "disabled", name: label });
        rejectReady(new Error(`MCP server "${label}" is disabled`));
        failureMap.set(raw, { spec: raw, name: label, reason: "disabled by user", at: Date.now() });
        return { ok: false, reason: "disabled by user" };
      }
      sink({ kind: "handshake", name: label });
      const t0 = Date.now();
      const namePrefix = spec.name
        ? `${spec.name}_`
        : ctx.getRequestedCount() === 1 && ctx.getMcpPrefix()
          ? (ctx.getMcpPrefix() as string)
          : "";
      if (spec.transport === "stdio") preflightStdioSpec(spec);
      const workspaceDir = ctx.getWorkspaceDir?.();
      const transport = buildTransportFromSpec(spec, { cwd: workspaceDir });
      mcp = new McpClient({ transport, workspaceDir });
      await mcp.initialize({ signal });
      const host: McpClientHost = { client: mcp };
      const bridge = await bridgeMcpTools(mcp, {
        registry: tools,
        namePrefix,
        serverName: label,
        host,
        ready,
        onProgress: (info) => ctx.progressSink.current?.(info),
        onSlow: (info) =>
          sink({
            kind: "slow",
            serverName: info.serverName,
            p95Ms: info.p95Ms,
            sampleSize: info.sampleSize,
          }),
      });
      // Tools are registered — record the bridge NOW so the UI shows
      // "bridged" even if later non-critical steps (inspect, hot-add) fail.
      const ms = Date.now() - t0;
      const allSpecs = tools.specs();
      const registeredSpecs = allSpecs.filter((s) =>
        bridge.registeredNames.includes(s.function.name),
      );
      // Create a provisional record immediately (tools already usable).
      records.set(raw, {
        spec: raw,
        client: mcp,
        summary: buildMcpServerSummary({
          label,
          spec: raw,
          toolCount: bridge.registeredNames.length,
          report: {
            protocolVersion: mcp.protocolVersion,
            serverInfo: mcp.serverInfo,
            capabilities: mcp.serverCapabilities ?? {},
            tools: { supported: true, items: [] },
            resources: { supported: false, reason: "still inspecting" },
            prompts: { supported: false, reason: "still inspecting" },
            elapsedMs: ms,
          },
          host,
          bridgeEnv: bridge.env,
        }),
        registeredNames: bridge.registeredNames,
        registeredSpecs,
      });
      insertionOrder.push(raw);
      resolveReady();
      sink({
        kind: "tools-ready",
        name: label,
        tools: bridge.registeredNames.length,
        ms,
      });

      // Non-critical: inspect + hot-add. Failures here don't un-bridge.
      let report: InspectionReport;
      try {
        report = await inspectMcpServer(mcp);
      } catch {
        report = {
          protocolVersion: mcp.protocolVersion,
          serverInfo: mcp.serverInfo,
          capabilities: mcp.serverCapabilities ?? {},
          tools: { supported: true, items: [] },
          resources: { supported: false, reason: "inspect failed" },
          prompts: { supported: false, reason: "inspect failed" },
          elapsedMs: 0,
        };
      }
      const resourceCount = report.resources.supported ? report.resources.items.length : 0;
      const promptCount = report.prompts.supported ? report.prompts.items.length : 0;
      // Re-emit with full inspection data (the provisional event reported 0).
      sink({
        kind: "connected",
        name: label,
        tools: bridge.registeredNames.length,
        resources: resourceCount,
        prompts: promptCount,
        ms,
      });
      const summary = buildMcpServerSummary({
        label,
        spec: raw,
        toolCount: bridge.registeredNames.length,
        report,
        host,
        bridgeEnv: bridge.env,
      });
      // Replace the provisional record with the fully-inspected summary.
      records.set(raw, {
        spec: raw,
        client: mcp,
        summary,
        registeredNames: bridge.registeredNames,
        registeredSpecs,
      });
      // Hot-add: shift the prefix so the live loop sees the new tools
      // on the very next turn. Each addTool is one cache-miss turn.
      if (loop)
        for (const s of registeredSpecs)
          try {
            loop.prefix.addTool(s);
          } catch (err) {
            sink({
              kind: "warn",
              name: label,
              reason: `addTool failed for ${s.function.name}: ${(err as Error).message}`,
            });
          }
      return { ok: true, summary };
    } catch (err) {
      // If we got far enough to create a provisional record, keep it —
      // tools are already registered and usable even after a late failure.
      const reason = (err as Error).message;
      if (!records.has(raw)) {
        await mcp?.close().catch(() => undefined);
        rejectReady(new Error(`MCP server "${label}" failed to start: ${reason}`));
        sink({ kind: "failed", name: label, reason });
        failureMap.set(raw, { spec: raw, name: label, reason, at: Date.now() });
        return { ok: false, reason };
      }
      sink({ kind: "warn", name: label, reason });
      return { ok: true, summary: records.get(raw)!.summary };
    }
  }

  async function removeSpec(raw: string, loop?: CacheFirstLoop): Promise<boolean> {
    failureMap.delete(raw);
    const record = records.get(raw);
    if (!record) return false;
    await record.client.close().catch(() => undefined);
    const tools = ctx.getTools();
    for (const name of record.registeredNames) {
      tools?.unregister(name);
      loop?.prefix.removeTool(name);
    }
    records.delete(raw);
    const idx = insertionOrder.indexOf(raw);
    if (idx >= 0) insertionOrder.splice(idx, 1);
    return true;
  }

  async function reloadFromConfig(loop?: CacheFirstLoop): Promise<{
    added: string[];
    removed: string[];
    failed: Array<{ spec: string; reason: string }>;
    summaries: McpServerSummary[];
  }> {
    const normalized = normalizeMcpConfig(readConfig());
    const desired = normalized.map(specToRaw);
    const desiredSet = new Set(desired);
    const currentSet = new Set(records.keys());
    const added: string[] = [];
    const removed: string[] = [];
    const failed: Array<{ spec: string; reason: string }> = [];

    for (const spec of [...currentSet]) {
      if (!desiredSet.has(spec)) {
        await removeSpec(spec, loop);
        removed.push(spec);
      }
    }
    for (const spec of desired) {
      if (currentSet.has(spec)) continue;
      const result = await addSpec(spec, loop);
      if (result.ok) added.push(spec);
      else failed.push({ spec, reason: result.reason });
    }
    return { added, removed, failed, summaries: summaries() };
  }

  function specs(): string[] {
    return [...insertionOrder];
  }
  function summaries(): McpServerSummary[] {
    return insertionOrder
      .map((s) => records.get(s)?.summary)
      .filter((s): s is McpServerSummary => Boolean(s));
  }
  async function closeAll(): Promise<void> {
    for (const r of records.values()) await r.client.close().catch(() => undefined);
    records.clear();
    insertionOrder.length = 0;
    failureMap.clear();
  }
  function failures(): McpFailure[] {
    return [...failureMap.values()];
  }
  function setLifecycleSink(s: McpLifecycleSink): void {
    sink = s;
  }
  return {
    size: () => records.size,
    specs,
    summaries,
    failures,
    addSpec,
    removeSpec,
    reloadFromConfig,
    closeAll,
    setLifecycleSink,
  };
}
