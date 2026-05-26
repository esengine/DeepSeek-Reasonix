import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { homedir } from "node:os";
import { basename, extname, join } from "node:path";
import {
  detectGitBranch,
  loadSessionMeta,
  patchSessionMeta,
  rewriteSession,
  sessionPath,
  sessionsDir,
} from "./memory/session.js";
import type { ChatMessage, ToolCall } from "./types.js";

export type ExternalSessionSource = "claude" | "codex";

export interface ImportedSession {
  messages: ChatMessage[];
  workspace?: string;
  nameHint?: string;
  summary?: string;
}

export interface ImportExternalSessionOptions {
  source: ExternalSessionSource;
  path: string;
  name?: string;
  workspace?: string;
  summary?: string;
  force?: boolean;
}

export interface ImportExternalSessionResult {
  source: ExternalSessionSource;
  path: string;
  name: string;
  messageCount: number;
  workspace?: string;
  summary?: string;
  branch?: string;
}

export interface ExternalSessionApp {
  source: ExternalSessionSource;
  label: string;
  root: string;
  available: boolean;
  sessionCount: number;
  latestMtime?: string;
}

export interface ImportExternalSessionsResult {
  imported: number;
  skipped: number;
  failed: number;
  latestName?: string;
}

interface ExternalSessionFile {
  source: ExternalSessionSource;
  path: string;
  mtimeMs: number;
}

interface ClaudeRecord {
  type?: unknown;
  isMeta?: unknown;
  cwd?: unknown;
  project?: unknown;
  message?: {
    role?: unknown;
    content?: unknown;
  };
}

interface CodexRecord {
  type?: unknown;
  payload?: {
    type?: unknown;
    role?: unknown;
    cwd?: unknown;
    message?: unknown;
    content?: unknown;
  };
}

export function parseExternalSessionFile(
  source: ExternalSessionSource,
  path: string,
): ImportedSession {
  if (!existsSync(path)) {
    throw new Error(`source file not found: ${path}`);
  }
  return source === "claude" ? parseClaudeSessionFile(path) : parseCodexSessionFile(path);
}

export function buildImportedSessionName(
  source: ExternalSessionSource,
  path: string,
  imported: ImportedSession,
): string {
  const stem = basename(path, extname(path));
  const hint = oneLine(imported.nameHint || imported.summary || stem, 48);
  return `${source}-${hint || stem || "session"}`;
}

export function importExternalSession(
  opts: ImportExternalSessionOptions,
): ImportExternalSessionResult {
  const imported = parseExternalSessionFile(opts.source, opts.path);
  if (imported.messages.length === 0) {
    throw new Error(`no importable chat messages found in ${opts.path}`);
  }

  const name = opts.name?.trim() || buildImportedSessionName(opts.source, opts.path, imported);
  const outputPath = sessionPath(name);
  if (existsSync(outputPath) && !opts.force) {
    throw new Error(`target session already exists: ${name}`);
  }

  rewriteSession(name, imported.messages);

  const workspace = opts.workspace?.trim() || imported.workspace;
  const summary = opts.summary?.trim() || imported.summary;
  const branch = workspace ? detectGitBranch(workspace) : undefined;
  patchSessionMeta(name, {
    workspace,
    summary,
    branch,
    importedSource: opts.source,
    importedPath: opts.path,
  });

  return {
    source: opts.source,
    path: opts.path,
    name,
    messageCount: imported.messages.length,
    workspace,
    summary,
    branch,
  };
}

export function discoverExternalSessionApps(): ExternalSessionApp[] {
  return (["claude", "codex"] as const).map((source) => {
    const root = defaultSessionRoot(source);
    const files = scanExternalSessionFiles(source);
    const latest = files[0];
    return {
      source,
      label: source === "claude" ? "Claude Code" : "Codex",
      root,
      available: files.length > 0,
      sessionCount: files.length,
      latestMtime: latest ? new Date(latest.mtimeMs).toISOString() : undefined,
    };
  });
}

export function importExternalSessions(opts: {
  sources: ExternalSessionSource[];
  workspace?: string;
}): ImportExternalSessionsResult {
  let imported = 0;
  let skipped = 0;
  let failed = 0;
  let latestName: string | undefined;

  const existing = importedPathKeys();
  for (const source of opts.sources) {
    const files = scanExternalSessionFiles(source);
    for (const file of files) {
      const key = importKey(source, file.path);
      if (existing.has(key)) {
        skipped++;
        continue;
      }
      try {
        const result = importExternalSession({
          source,
          path: file.path,
          workspace: opts.workspace,
        });
        existing.add(key);
        imported++;
        latestName ||= result.name;
      } catch {
        failed++;
      }
    }
  }

  return { imported, skipped, failed, latestName };
}

function defaultSessionRoot(source: ExternalSessionSource): string {
  return source === "claude"
    ? join(homedir(), ".claude", "projects")
    : join(homedir(), ".codex", "sessions");
}

function scanExternalSessionFiles(source: ExternalSessionSource): ExternalSessionFile[] {
  const root = defaultSessionRoot(source);
  const out: ExternalSessionFile[] = [];
  collectJsonl(root, source, out);
  out.sort((a, b) => b.mtimeMs - a.mtimeMs);
  return out;
}

function collectJsonl(
  dir: string,
  source: ExternalSessionSource,
  out: ExternalSessionFile[],
): void {
  if (!existsSync(dir)) return;
  let entries: string[];
  try {
    entries = readdirSync(dir);
  } catch {
    return;
  }
  for (const entry of entries) {
    const path = join(dir, entry);
    let stat: ReturnType<typeof statSync>;
    try {
      stat = statSync(path);
    } catch {
      continue;
    }
    if (stat.isDirectory()) {
      collectJsonl(path, source, out);
    } else if (stat.isFile() && entry.endsWith(".jsonl")) {
      out.push({ source, path, mtimeMs: stat.mtimeMs });
    }
  }
}

function importedPathKeys(): Set<string> {
  const out = new Set<string>();
  const dir = sessionsDir();
  if (!existsSync(dir)) return out;
  let files: string[];
  try {
    files = readdirSync(dir);
  } catch {
    return out;
  }
  for (const file of files) {
    if (!file.endsWith(".jsonl") || file.endsWith(".events.jsonl")) continue;
    const name = file.replace(/\.jsonl$/, "");
    const meta = loadSessionMeta(name);
    if (meta.importedSource && meta.importedPath) {
      out.add(importKey(meta.importedSource, meta.importedPath));
    }
  }
  return out;
}

function importKey(source: ExternalSessionSource, path: string): string {
  return `${source}:${path}`;
}

function parseClaudeSessionFile(path: string): ImportedSession {
  const records = readJsonl(path) as ClaudeRecord[];
  const messages: ChatMessage[] = [];
  const toolNames = new Map<string, string>();
  let workspace: string | undefined;
  let firstUserText: string | undefined;

  for (const record of records) {
    if (!workspace) workspace = firstString(record.cwd) || firstString(record.project);
    if (record.isMeta === true) continue;
    if (!record.message || typeof record.message !== "object") continue;
    const role = normalizeRole(record.message.role);
    if (!role) continue;

    if (role === "assistant") {
      const assistant = normalizeClaudeAssistant(record.message.content);
      for (const call of assistant.toolCalls) {
        if (call.id && call.function?.name) toolNames.set(call.id, call.function.name);
      }
      if (assistant.content || assistant.toolCalls.length > 0) {
        messages.push({
          role: "assistant",
          content: assistant.content || null,
          tool_calls: assistant.toolCalls.length > 0 ? assistant.toolCalls : undefined,
          reasoning_content: assistant.reasoning || undefined,
        });
      }
      continue;
    }

    const user = normalizeClaudeUser(record.message.content, toolNames);
    if (user.content) {
      messages.push({ role: "user", content: user.content });
      if (!firstUserText) firstUserText = user.content;
    }
    messages.push(...user.toolMessages);
  }

  return {
    messages,
    workspace,
    nameHint: firstUserText,
    summary: summarize(firstUserText),
  };
}

function parseCodexSessionFile(path: string): ImportedSession {
  const records = readJsonl(path) as CodexRecord[];
  const messages: ChatMessage[] = [];
  const fallback: ChatMessage[] = [];
  let workspace: string | undefined;
  let firstUserText: string | undefined;

  for (const record of records) {
    if (record.type === "session_meta" || record.type === "turn_context") {
      workspace ||= firstString(record.payload?.cwd);
    }

    if (record.type === "response_item" && record.payload?.type === "message") {
      const role = normalizeRole(record.payload.role);
      if (!role) continue;
      const content = normalizeCodexMessageContent(role, record.payload.content);
      if (!content) continue;
      messages.push({ role, content });
      if (role === "user" && !firstUserText) firstUserText = content;
      continue;
    }

    if (record.type === "event_msg") {
      const eventType = firstString(record.payload?.type);
      const content = firstString(record.payload?.message);
      if (!content) continue;
      if (eventType === "user_message") {
        fallback.push({ role: "user", content });
        if (!firstUserText) firstUserText = content;
      } else if (eventType === "agent_message") {
        fallback.push({ role: "assistant", content });
      }
    }
  }

  const importedMessages = messages.length > 0 ? messages : dedupeAdjacentMessages(fallback);
  return {
    messages: importedMessages,
    workspace,
    nameHint: firstUserText,
    summary: summarize(firstUserText),
  };
}

function normalizeClaudeAssistant(content: unknown): {
  content: string;
  toolCalls: ToolCall[];
  reasoning?: string;
} {
  if (typeof content === "string") {
    return { content: content.trim(), toolCalls: [] };
  }
  if (!Array.isArray(content)) {
    return { content: "", toolCalls: [] };
  }

  const textParts: string[] = [];
  const toolCalls: ToolCall[] = [];
  const reasoningParts: string[] = [];

  for (const item of content) {
    if (!item || typeof item !== "object") continue;
    const type = firstString((item as Record<string, unknown>).type);
    if (type === "text") {
      const text = firstString((item as Record<string, unknown>).text);
      if (text) textParts.push(text);
      continue;
    }
    if (type === "thinking") {
      const text = firstString((item as Record<string, unknown>).thinking);
      if (text) reasoningParts.push(text);
      continue;
    }
    if (type === "tool_use") {
      const name = firstString((item as Record<string, unknown>).name);
      if (!name) continue;
      toolCalls.push({
        id: firstString((item as Record<string, unknown>).id),
        type: "function",
        function: {
          name,
          arguments: safeJson((item as Record<string, unknown>).input ?? {}),
        },
      });
    }
  }

  return {
    content: joinParts(textParts),
    toolCalls,
    reasoning: joinParts(reasoningParts) || undefined,
  };
}

function normalizeClaudeUser(
  content: unknown,
  toolNames: ReadonlyMap<string, string>,
): { content: string; toolMessages: ChatMessage[] } {
  if (typeof content === "string") {
    return { content: content.trim(), toolMessages: [] };
  }
  if (!Array.isArray(content)) {
    return { content: "", toolMessages: [] };
  }

  const userText: string[] = [];
  const toolMessages: ChatMessage[] = [];

  for (const item of content) {
    if (!item || typeof item !== "object") continue;
    const type = firstString((item as Record<string, unknown>).type);
    if (type === "text") {
      const text = firstString((item as Record<string, unknown>).text);
      if (text) userText.push(text);
      continue;
    }
    if (type === "image") {
      userText.push("[image omitted]");
      continue;
    }
    if (type === "tool_result") {
      const callId = firstString((item as Record<string, unknown>).tool_use_id);
      toolMessages.push({
        role: "tool",
        content: normalizeArbitraryContent((item as Record<string, unknown>).content),
        tool_call_id: callId,
        name: callId ? toolNames.get(callId) : undefined,
      });
    }
  }

  return { content: joinParts(userText), toolMessages };
}

function normalizeCodexMessageContent(role: "user" | "assistant", content: unknown): string {
  if (typeof content === "string") return content.trim();
  if (!Array.isArray(content)) return "";
  const textParts: string[] = [];
  for (const item of content) {
    if (!item || typeof item !== "object") continue;
    const type = firstString((item as Record<string, unknown>).type);
    if (type === "input_text" || type === "output_text" || type === "text") {
      const text = firstString((item as Record<string, unknown>).text);
      if (!text) continue;
      if (role === "user" && looksLikeCodexBootstrapBlock(text)) continue;
      textParts.push(text);
    }
  }
  return joinParts(textParts);
}

function looksLikeCodexBootstrapBlock(text: string): boolean {
  const trimmed = text.trimStart();
  return (
    trimmed.startsWith("# AGENTS.md instructions for ") ||
    trimmed.startsWith("<environment_context>")
  );
}

function dedupeAdjacentMessages(messages: ChatMessage[]): ChatMessage[] {
  const out: ChatMessage[] = [];
  for (const msg of messages) {
    const prev = out[out.length - 1];
    if (prev && prev.role === msg.role && prev.content === msg.content) continue;
    out.push(msg);
  }
  return out;
}

function normalizeArbitraryContent(value: unknown): string {
  if (typeof value === "string") return value;
  if (Array.isArray(value)) {
    const textParts = value
      .map((item) => {
        if (typeof item === "string") return item;
        if (item && typeof item === "object") {
          return firstString((item as Record<string, unknown>).text);
        }
        return "";
      })
      .filter(Boolean) as string[];
    if (textParts.length > 0) return joinParts(textParts);
  }
  return safeJson(value);
}

function readJsonl(path: string): unknown[] {
  const raw = readFileSync(path, "utf8");
  return raw
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean)
    .flatMap((line) => {
      try {
        return [JSON.parse(line)];
      } catch {
        return [];
      }
    });
}

function normalizeRole(value: unknown): "user" | "assistant" | undefined {
  return value === "user" || value === "assistant" ? value : undefined;
}

function firstString(value: unknown): string | undefined {
  return typeof value === "string" && value.trim().length > 0 ? value : undefined;
}

function safeJson(value: unknown): string {
  try {
    return JSON.stringify(value ?? null);
  } catch {
    return JSON.stringify(String(value));
  }
}

function joinParts(parts: string[]): string {
  return parts
    .map((part) => part.trim())
    .filter(Boolean)
    .join("\n\n");
}

function summarize(text: string | undefined): string | undefined {
  const flat = oneLine(text || "", 120);
  return flat || undefined;
}

function oneLine(text: string, max: number): string {
  const flat = text.replace(/\s+/g, " ").trim();
  return flat.length > max ? `${flat.slice(0, max)}...` : flat;
}
