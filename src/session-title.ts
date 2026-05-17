import { existsSync } from "node:fs";
import { type SessionMeta, sanitizeName, sessionPath, timestampSuffix } from "./memory/session.js";
import type { ChatMessage, ChatRequestOptions } from "./types.js";

const TITLE_MODEL_MAX_TOKENS = 32;
const TITLE_MAX_CHARS = 48;

export interface SessionTitleInput {
  workspace?: string;
  userText: string;
  assistantText?: string;
}

export interface SessionTitleClient {
  chat(opts: ChatRequestOptions): Promise<{ content: string }>;
}

export function buildSessionTitleMessages(input: SessionTitleInput): ChatMessage[] {
  const workspace = input.workspace?.trim();
  const assistant = input.assistantText?.trim();
  const parts = [
    workspace ? `Workspace: ${workspace}` : "",
    `User request:\n${truncateForPrompt(input.userText, 1600)}`,
    assistant ? `Assistant answer:\n${truncateForPrompt(assistant, 1600)}` : "",
  ].filter(Boolean);
  return [
    {
      role: "system",
      content:
        "Generate a short session title for a coding/chat transcript. Output only the title, no quotes, no markdown, no prefix. Use the user's language when obvious. Keep it under 6 words or 18 CJK characters. Avoid punctuation.",
    },
    { role: "user", content: parts.join("\n\n") },
  ];
}

export async function generateSessionTitle(
  client: SessionTitleClient,
  model: string,
  input: SessionTitleInput,
): Promise<string | null> {
  const resp = await client.chat({
    model,
    messages: buildSessionTitleMessages(input),
    temperature: 0.2,
    maxTokens: TITLE_MODEL_MAX_TOKENS,
    thinking: "disabled",
  });
  return normalizeGeneratedSessionTitle(resp.content);
}

export function normalizeGeneratedSessionTitle(raw: string | null | undefined): string | null {
  if (!raw) return null;
  let title = raw.trim();
  title = title.replace(/^```[a-zA-Z0-9_-]*\s*/, "").replace(/\s*```$/, "");
  title = title.split(/\r?\n/)[0]?.trim() ?? "";
  title = title.replace(/^(title|session title|name)\s*[:：-]\s*/i, "");
  title = title.replace(/^["'“”‘’`]+|["'“”‘’`]+$/g, "");
  title = title.replace(/\s+/g, " ").trim();
  title = title.replace(/[。.!?！？；;，,、]+$/g, "").trim();
  if (!title) return null;
  return title.length > TITLE_MAX_CHARS ? title.slice(0, TITLE_MAX_CHARS).trim() : title;
}

export function makeSessionNameFromTitle(
  title: string | null | undefined,
  opts: {
    currentName?: string;
    exists?: (name: string) => boolean;
    suffix?: () => string;
  } = {},
): string | null {
  const normalized = normalizeGeneratedSessionTitle(title);
  if (!normalized) return null;
  const base = sanitizeName(
    normalized
      .replace(/[\s_]+/g, "-")
      .replace(/[^\w\-\u4e00-\u9fa5]+/g, "-")
      .replace(/-+/g, "-")
      .replace(/^-|-$/g, ""),
  );
  if (!base) return null;
  const current = opts.currentName ? sanitizeName(opts.currentName) : "";
  const exists = opts.exists ?? ((name: string) => existsSync(sessionPath(name)));
  if (base === current || !exists(base)) return base;
  for (let i = 2; i <= 9; i++) {
    const candidate = `${base}-${i}`;
    if (candidate === current || !exists(candidate)) return candidate;
  }
  return `${base}-${opts.suffix?.() ?? timestampSuffix()}`;
}

export function shouldAutoNameSession(
  sessionName: string | undefined,
  meta: SessionMeta,
  completedTurns: number,
): boolean {
  if (!sessionName || completedTurns !== 1 || meta.autoTitleGenerated) return false;
  return /^default(?:-\d{12,14})?$/.test(sanitizeName(sessionName));
}

function truncateForPrompt(text: string, max: number): string {
  const trimmed = text.replace(/\s+/g, " ").trim();
  return trimmed.length > max ? `${trimmed.slice(0, max)}...` : trimmed;
}
