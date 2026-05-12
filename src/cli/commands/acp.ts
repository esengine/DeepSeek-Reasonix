/** ACP (Agent Client Protocol) agent — drives the cache-first loop over stdio NDJSON JSON-RPC. */

import { existsSync, statSync } from "node:fs";
import { resolve } from "node:path";
import { dispatchKernelEvent } from "../../acp/dispatch.js";
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
import { loadApiKey, loadBaseUrl, loadPreset, loadReasoningEffort } from "../../config.js";
import { Eventizer } from "../../core/eventize.js";
import { loadDotenv } from "../../env.js";
import { CacheFirstLoop, DeepSeekClient, ImmutablePrefix } from "../../index.js";
import { timestampSuffix } from "../../memory/session.js";
import { VERSION } from "../../version.js";
import { canonicalPresetName, resolvePreset } from "../ui/presets.js";

export interface AcpOptions {
  model?: string;
  dir?: string;
  budgetUsd?: number;
}

interface Session {
  id: string;
  rootDir: string;
  model: string;
  toolset: Awaited<ReturnType<typeof buildCodeToolset>>;
  loop: CacheFirstLoop;
  eventizer: Eventizer;
  ctx: { model: string; prefixHash: string; reasoningEffort: "high" | "max" };
  aborter: AbortController | null;
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
}): Promise<Session> {
  const preset = canonicalPresetName(loadPreset());
  const resolved = resolvePreset(preset);
  const model = opts.modelOverride || resolved.model;
  const toolset = await buildCodeToolset({ rootDir: opts.rootDir });
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
  const server = new AcpServer();

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
      for await (const ev of session.loop.step(text)) {
        if (session.aborter.signal.aborted) {
          stopReason = "cancelled";
          break;
        }
        for (const kev of session.eventizer.consume(ev, session.ctx)) {
          dispatchKernelEvent(server, session.id, kev);
          if (kev.type === "error") stopReason = "error";
        }
      }
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

  await server.done();
}
