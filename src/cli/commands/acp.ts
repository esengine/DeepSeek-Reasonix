/** ACP (Agent Client Protocol) agent — drives the cache-first loop over stdio NDJSON JSON-RPC. */

import { AsyncLocalStorage } from "node:async_hooks";
import { type WriteStream, existsSync, statSync } from "node:fs";
import { resolve } from "node:path";
import { dispatchKernelEvent } from "../../acp/dispatch.js";
import { requestPermissionForGate } from "../../acp/gates.js";
import {
  ACP_PROTOCOL_VERSION,
  type ContentBlock,
  ERR_INVALID_PARAMS,
  type InitializeParams,
  type InitializeResult,
  type SessionCancelParams,
  type SessionNewParams,
  type SessionNewResult,
  type SessionPromptParams,
  type SessionPromptResult,
  type SessionUpdateParams,
  type StopReason,
  flattenPrompt,
} from "../../acp/protocol.js";
import { AcpServer } from "../../acp/server.js";
import { codeSystemPrompt } from "../../code/prompt.js";
import { buildCodeToolset } from "../../code/setup.js";
import {
  loadApiKey,
  loadBaseUrl,
  loadPreset,
  loadReasoningEffort,
  mcpEnvFor,
  readConfig,
} from "../../config.js";
import { loadEditMode } from "../../config.js";
import { Eventizer } from "../../core/eventize.js";
import { pauseGate } from "../../core/pause-gate.js";
import { autoResolveVerdict } from "../../core/pause-policy.js";
import { loadDotenv } from "../../env.js";
import { t } from "../../i18n/index.js";
import { CacheFirstLoop, DeepSeekClient, ImmutablePrefix } from "../../index.js";
import { McpClient } from "../../mcp/client.js";
import { preflightStdioSpec } from "../../mcp/preflight.js";
import { bridgeMcpTools } from "../../mcp/registry.js";
import { parseMcpSpec } from "../../mcp/spec.js";
import { buildTransportFromSpec } from "../../mcp/transport-from-spec.js";
import { timestampSuffix } from "../../memory/session.js";
import { openTranscriptFile, recordFromLoopEvent, writeRecord } from "../../transcript/log.js";
import { VERSION } from "../../version.js";
import { formatMcpLifecycleEvent } from "../ui/mcp-lifecycle.js";
import { formatMcpSlowToast } from "../ui/mcp-toast.js";
import { canonicalPresetName, resolvePreset } from "../ui/presets.js";

export interface AcpOptions {
  model?: string;
  dir?: string;
  budgetUsd?: number;
  transcript?: string;
  yolo?: boolean;
  /** Zero or more MCP server specs. Each: `"name=cmd args..."` or `"cmd args..."`. */
  mcpSpecs?: string[];
  /** Global prefix — only honored when a single anonymous server is given. */
  mcpPrefix?: string;
}

interface Session {
  id: string;
  rootDir: string;
  model: string;
  toolset: Awaited<ReturnType<typeof buildCodeToolset>>;
  mcpClients: McpClient[];
  loop: CacheFirstLoop;
  eventizer: Eventizer;
  ctx: { model: string; prefixHash: string; reasoningEffort: "high" | "max" };
  aborter: AbortController | null;
}

function resolveMcpPrefix(
  specName: string | null | undefined,
  specCount: number,
  globalPrefix: string | undefined,
): string {
  if (specName) return `${specName}_`;
  if (specCount === 1 && globalPrefix) return globalPrefix;
  return "";
}

// Mirrors run.ts:81-142.
export async function loadMcpServers(
  tools: import("../../tools.js").ToolRegistry,
  specs: string[],
  globalPrefix: string | undefined,
): Promise<McpClient[]> {
  const clients: McpClient[] = [];
  if (specs.length === 0) return clients;
  const cfg = readConfig();
  const disabledNames = new Set(cfg.mcpDisabled ?? []);
  for (const raw of specs) {
    let label = "anon";
    let mcp: McpClient | undefined;
    try {
      const spec = parseMcpSpec(raw);
      label = spec.name ?? "anon";
      if (spec.name && disabledNames.has(spec.name)) {
        process.stderr.write(`${formatMcpLifecycleEvent({ state: "disabled", name: label })}\n`);
        continue;
      }
      process.stderr.write(`${formatMcpLifecycleEvent({ state: "handshake", name: label })}\n`);
      const t0 = Date.now();
      const prefix = resolveMcpPrefix(spec.name, specs.length, globalPrefix);
      if (spec.transport === "stdio") preflightStdioSpec(spec);
      const transport = buildTransportFromSpec(spec, { env: mcpEnvFor(spec.name, cfg) });
      mcp = new McpClient({ transport });
      await mcp.initialize();
      const bridge = await bridgeMcpTools(mcp, {
        registry: tools,
        namePrefix: prefix,
        serverName: label,
        onSlow: (info) =>
          process.stderr.write(
            `${formatMcpSlowToast({ name: info.serverName, p95Ms: info.p95Ms, sampleSize: info.sampleSize })}\n`,
          ),
      });
      process.stderr.write(
        `${formatMcpLifecycleEvent({
          state: "connected",
          name: label,
          tools: bridge.registeredNames.length,
          ms: Date.now() - t0,
        })}\n`,
      );
      clients.push(mcp);
    } catch (err) {
      await mcp?.close().catch(() => undefined);
      process.stderr.write(
        `${formatMcpLifecycleEvent({ state: "failed", name: label, reason: (err as Error).message })}\n  → ${t("mcpLifecycle.failedSetupConfigHint")}\n`,
      );
    }
  }
  return clients;
}

function resolveDir(raw: string | undefined, fallback: string): string {
  if (!raw) return fallback;
  const abs = resolve(raw);
  if (!existsSync(abs) || !statSync(abs).isDirectory()) {
    throw new Error(`workspace directory not found: ${abs}`);
  }
  return abs;
}

async function buildSession(opts: {
  rootDir: string;
  modelOverride?: string;
  budgetUsd?: number;
  mcpSpecs?: string[];
  mcpPrefix?: string;
}): Promise<Session> {
  const preset = canonicalPresetName(loadPreset());
  const resolved = resolvePreset(preset);
  const model = opts.modelOverride || resolved.model;
  const toolset = await buildCodeToolset({ rootDir: opts.rootDir });
  // Bridge MCP tools BEFORE building the prefix so their specs make it into the cache key.
  const mcpClients = await loadMcpServers(toolset.tools, opts.mcpSpecs ?? [], opts.mcpPrefix);
  const system = codeSystemPrompt(opts.rootDir, {
    hasSemanticSearch: toolset.semantic.enabled,
    modelId: model,
  });
  const client = new DeepSeekClient({ baseUrl: loadBaseUrl() });
  const prefix = new ImmutablePrefix({ system, toolSpecs: toolset.tools.specs() });
  const loop = new CacheFirstLoop({
    client,
    prefix,
    tools: toolset.tools,
    model,
    budgetUsd: opts.budgetUsd,
    session: `acp-${timestampSuffix()}`,
  });
  return {
    id: `sess_${timestampSuffix()}-${Math.random().toString(36).slice(2, 8)}`,
    rootDir: opts.rootDir,
    model,
    toolset,
    mcpClients,
    loop,
    eventizer: new Eventizer(),
    ctx: {
      model,
      prefixHash: prefix.fingerprint,
      reasoningEffort: loadReasoningEffort(),
    },
    aborter: null,
  };
}

export async function acpCommand(opts: AcpOptions): Promise<void> {
  loadDotenv();
  if (loadApiKey()) {
    process.env.DEEPSEEK_API_KEY = loadApiKey();
  }

  const defaultDir = resolveDir(opts.dir, process.cwd());
  const sessions = new Map<string, Session>();
  const sessionContext = new AsyncLocalStorage<string>();
  const server = new AcpServer();

  let transcriptStream: WriteStream | null = null;
  if (opts.transcript) {
    const defaultModel = opts.model || resolvePreset(canonicalPresetName(loadPreset())).model;
    transcriptStream = openTranscriptFile(opts.transcript, {
      version: 1,
      source: "reasonix acp",
      model: defaultModel,
      startedAt: new Date().toISOString(),
    });
  }

  pauseGate.on((req) => {
    const editMode = opts.yolo ? "yolo" : loadEditMode();
    const auto = autoResolveVerdict(req, editMode);
    if (auto !== null) {
      pauseGate.resolve(req.id, auto);
      return;
    }
    const activeSessionId = sessionContext.getStore();
    if (!activeSessionId || !sessions.has(activeSessionId)) {
      pauseGate.cancel(req.id);
      return;
    }
    void (async () => {
      const verdict = await requestPermissionForGate(server, activeSessionId, req);
      pauseGate.resolve(req.id, verdict);
    })();
  });

  server.onRequest<InitializeParams, InitializeResult>("initialize", (params) => {
    if (!params || typeof params !== "object") {
      throw Object.assign(new Error("initialize: missing params"), { code: ERR_INVALID_PARAMS });
    }
    return {
      protocolVersion: ACP_PROTOCOL_VERSION,
      agentCapabilities: {
        loadSession: false,
        promptCapabilities: { image: false, audio: false, embeddedContext: true },
        mcpCapabilities: { http: false, sse: false },
      },
      agentInfo: { name: "reasonix", title: "Reasonix", version: VERSION },
      authMethods: [],
    };
  });

  server.onRequest<SessionNewParams, SessionNewResult>("session/new", async (params) => {
    const rootDir = resolveDir(params?.cwd, defaultDir);
    const session = await buildSession({
      rootDir,
      modelOverride: opts.model,
      budgetUsd: opts.budgetUsd,
      mcpSpecs: opts.mcpSpecs,
      mcpPrefix: opts.mcpPrefix,
    });
    sessions.set(session.id, session);
    return { sessionId: session.id };
  });

  server.onRequest<SessionPromptParams, SessionPromptResult>("session/prompt", async (params) => {
    if (!params?.sessionId) {
      throw Object.assign(new Error("session/prompt: missing sessionId"), {
        code: ERR_INVALID_PARAMS,
      });
    }
    const session = sessions.get(params.sessionId);
    if (!session) {
      throw Object.assign(new Error(`session/prompt: unknown session ${params.sessionId}`), {
        code: ERR_INVALID_PARAMS,
      });
    }
    const text = flattenPrompt(params.prompt as ContentBlock[]);
    if (!text) {
      throw Object.assign(new Error("session/prompt: empty prompt"), { code: ERR_INVALID_PARAMS });
    }
    session.aborter = new AbortController();
    let stopReason: StopReason = "end_turn";
    try {
      await sessionContext.run(session.id, async () => {
        for await (const ev of session.loop.step(text)) {
          if (session.aborter?.signal.aborted) {
            stopReason = "cancelled";
            break;
          }
          // transcript needs raw LoopEvent (usage/cost/stats); kernel events lose those fields
          if (transcriptStream) {
            writeRecord(
              transcriptStream,
              recordFromLoopEvent(ev, {
                model: session.ctx.model,
                prefixHash: session.ctx.prefixHash,
              }),
            );
          }
          for (const kev of session.eventizer.consume(ev, session.ctx)) {
            dispatchKernelEvent(server, session.id, kev);
            if (kev.type === "error") stopReason = "error";
          }
        }
      });
    } catch (err) {
      const message = (err as Error).message;
      server.sendNotification("session/update", {
        sessionId: session.id,
        update: {
          sessionUpdate: "agent_message_chunk",
          content: { type: "text", text: `\n\n[error] ${message}` },
        },
      } satisfies SessionUpdateParams);
      stopReason = "error";
    } finally {
      session.aborter = null;
    }
    return { stopReason };
  });

  server.onNotification<SessionCancelParams>("session/cancel", (params) => {
    const session = params?.sessionId ? sessions.get(params.sessionId) : undefined;
    session?.aborter?.abort();
  });

  try {
    await server.done();
  } finally {
    transcriptStream?.end();
    // Tear down MCP children so spawned servers don't outlive the agent.
    const closes: Promise<unknown>[] = [];
    for (const session of sessions.values()) {
      for (const mcp of session.mcpClients) {
        closes.push(mcp.close().catch(() => undefined));
      }
    }
    await Promise.all(closes);
  }
}
