import { type EventSourceMessage, createParser } from "eventsource-parser";
import { type RetryOptions, fetchWithRetry } from "./retry.js";
import type { ChatMessage, ChatRequestOptions, RawUsage, ToolCall, ToolSpec } from "./types.js";

export class Usage {
  constructor(
    public promptTokens = 0,
    public completionTokens = 0,
    public totalTokens = 0,
    public promptCacheHitTokens = 0,
    public promptCacheMissTokens = 0,
  ) {}

  get cacheHitRatio(): number {
    const denom = this.promptCacheHitTokens + this.promptCacheMissTokens;
    return denom > 0 ? this.promptCacheHitTokens / denom : 0;
  }

  static fromApi(raw: RawUsage | undefined | null): Usage {
    const u = raw ?? {};
    return new Usage(
      u.prompt_tokens ?? 0,
      u.completion_tokens ?? 0,
      u.total_tokens ?? 0,
      u.prompt_cache_hit_tokens ?? 0,
      u.prompt_cache_miss_tokens ?? 0,
    );
  }
}

export interface ChatResponse {
  content: string;
  reasoningContent: string | null;
  toolCalls: ToolCall[];
  usage: Usage;
  raw: unknown;
}

export interface StreamChunk {
  contentDelta?: string;
  reasoningDelta?: string;
  toolCallDelta?: { index: number; id?: string; name?: string; argumentsDelta?: string };
  usage?: Usage;
  finishReason?: string;
  raw: any;
}

export interface BalanceInfo {
  currency: string;
  total_balance: string;
  granted_balance?: string;
  topped_up_balance?: string;
}

export interface UserBalance {
  is_available: boolean;
  balance_infos: BalanceInfo[];
}

/** Largest `total_balance` wins — the wallet the user actually paid for and expects to see ticking down. */
export function pickPrimaryBalance(infos: ReadonlyArray<BalanceInfo>): BalanceInfo | null {
  if (infos.length === 0) return null;
  let best = infos[0]!;
  for (let i = 1; i < infos.length; i++) {
    if (Number(infos[i]!.total_balance) > Number(best.total_balance)) best = infos[i]!;
  }
  return best;
}

export interface ModelInfo {
  id: string;
  object: "model";
  owned_by: string;
}

export interface ModelList {
  object: "list";
  data: ModelInfo[];
}

export interface DeepSeekClientOptions {
  apiKey?: string;
  baseUrl?: string;
  timeoutMs?: number;
  fetch?: typeof fetch;
  /** Retry configuration. Pass `{ maxAttempts: 1 }` to disable retries. */
  retry?: RetryOptions;
}

export class DeepSeekClient {
  readonly apiKey: string;
  readonly baseUrl: string;
  readonly timeoutMs: number;
  readonly retry: RetryOptions;
  private readonly _fetch: typeof fetch;

  constructor(opts: DeepSeekClientOptions = {}) {
    const apiKey = opts.apiKey ?? process.env.DEEPSEEK_API_KEY;
    if (!apiKey) {
      throw new Error(
        "DEEPSEEK_API_KEY is not set. Put it in .env or pass apiKey to DeepSeekClient.",
      );
    }
    this.apiKey = apiKey;
    let url = opts.baseUrl ?? process.env.DEEPSEEK_BASE_URL ?? "https://api.deepseek.com";
    // Manual trim — `/\/+$/` is O(n²) on slash-heavy non-matches per CodeQL js/polynomial-redos.
    while (url.endsWith("/")) url = url.slice(0, -1);
    this.baseUrl = url;
    // 11 min. DeepSeek's load-balancer may keep a connection open for
    // up to 10 minutes while the request waits in queue (non-streaming
    // sends empty lines, streaming sends `:` SSE keep-alive comments —
    // both are invisible to our parsers, so neither surfaces until the
    // real response starts). Timing out at the legacy 2-min default
    // killed queued requests prematurely, burned the queue slot on
    // retry, and could loop through the whole queue repeatedly.
    // Setting 11 min lets the server's own 10-min cap close the
    // connection first (clean EOF → natural retry), and our timer
    // is a safety net for genuinely hung sockets.
    this.timeoutMs = opts.timeoutMs ?? 660_000;
    this._fetch = opts.fetch ?? globalThis.fetch.bind(globalThis);
    this.retry = opts.retry ?? {};
  }

  private buildPayload(opts: ChatRequestOptions, stream: boolean) {
    const payload: Record<string, unknown> = {
      model: opts.model,
      messages: opts.messages,
      stream,
    };
    if (opts.tools?.length) payload.tools = opts.tools;
    if (opts.temperature !== undefined) payload.temperature = opts.temperature;
    if (opts.maxTokens !== undefined) payload.max_tokens = opts.maxTokens;
    if (opts.responseFormat) payload.response_format = opts.responseFormat;
    // V4 thinking-mode toggle: lives under `extra_body.thinking.type` per
    // DeepSeek's docs. Docs also note that in thinking mode `temperature`,
    // `top_p`, `presence_penalty`, `frequency_penalty` are silently
    // ignored — we don't strip them here because the server's explicit
    // "setting won't report an error" contract means leaving them in is
    // safe and keeps the request payload diffable against OpenAI tooling.
    if (opts.thinking) {
      payload.extra_body = { thinking: { type: opts.thinking } };
    }
    if (opts.reasoningEffort) {
      payload.reasoning_effort = opts.reasoningEffort;
    }
    return payload;
  }

  /** Returns null on failure so callers can degrade — session must keep working without balance UI. */
  async getBalance(opts: { signal?: AbortSignal } = {}): Promise<UserBalance | null> {
    try {
      const resp = await this._fetch(`${this.baseUrl}/user/balance`, {
        method: "GET",
        headers: { Authorization: `Bearer ${this.apiKey}` },
        signal: opts.signal,
      });
      if (!resp.ok) return null;
      const data = (await resp.json()) as UserBalance;
      if (!data || !Array.isArray(data.balance_infos)) return null;
      return data;
    } catch {
      return null;
    }
  }

  /** Returns null on failure — callers fall back to a hardcoded model hint. */
  async listModels(opts: { signal?: AbortSignal } = {}): Promise<ModelList | null> {
    try {
      const resp = await this._fetch(`${this.baseUrl}/models`, {
        method: "GET",
        headers: { Authorization: `Bearer ${this.apiKey}` },
        signal: opts.signal,
      });
      if (!resp.ok) return null;
      const data = (await resp.json()) as ModelList;
      if (!data || !Array.isArray(data.data)) return null;
      return data;
    } catch {
      return null;
    }
  }

  async chat(opts: ChatRequestOptions): Promise<ChatResponse> {
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), this.timeoutMs);
    const signal = opts.signal ?? ctrl.signal;

    try {
      const resp = await fetchWithRetry(
        this._fetch,
        `${this.baseUrl}/chat/completions`,
        {
          method: "POST",
          headers: {
            Authorization: `Bearer ${this.apiKey}`,
            "Content-Type": "application/json",
          },
          body: JSON.stringify(this.buildPayload(opts, false)),
          signal,
        },
        { ...this.retry, signal },
      );
      if (!resp.ok) {
        throw new Error(`DeepSeek ${resp.status}: ${await resp.text()}`);
      }
      const data: any = await resp.json();
      const choice = data.choices?.[0]?.message ?? {};
      return {
        content: choice.content ?? "",
        reasoningContent: choice.reasoning_content ?? null,
        toolCalls: choice.tool_calls ?? [],
        usage: Usage.fromApi(data.usage),
        raw: data,
      };
    } finally {
      clearTimeout(timer);
    }
  }

  async *stream(opts: ChatRequestOptions): AsyncGenerator<StreamChunk> {
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), this.timeoutMs);
    const signal = opts.signal ?? ctrl.signal;

    let resp: Response;
    try {
      // Only the initial fetch is retried. Once the server has started sending
      // the stream body we do NOT retry — a mid-stream retry would re-bill and
      // desync the session context.
      resp = await fetchWithRetry(
        this._fetch,
        `${this.baseUrl}/chat/completions`,
        {
          method: "POST",
          headers: {
            Authorization: `Bearer ${this.apiKey}`,
            "Content-Type": "application/json",
            Accept: "text/event-stream",
          },
          body: JSON.stringify(this.buildPayload(opts, true)),
          signal,
        },
        { ...this.retry, signal },
      );
    } catch (err) {
      clearTimeout(timer);
      throw err;
    }
    if (!resp.ok || !resp.body) {
      clearTimeout(timer);
      throw new Error(`DeepSeek ${resp.status}: ${await resp.text().catch(() => "")}`);
    }

    const queue: StreamChunk[] = [];
    let done = false;
    const parser = createParser({
      onEvent: (ev: EventSourceMessage) => {
        if (!ev.data || ev.data === "[DONE]") {
          done = true;
          return;
        }
        try {
          const json = JSON.parse(ev.data);
          const delta = json.choices?.[0]?.delta ?? {};
          const finishReason = json.choices?.[0]?.finish_reason ?? undefined;
          const chunk: StreamChunk = { raw: json, finishReason };
          if (typeof delta.content === "string" && delta.content.length > 0) {
            chunk.contentDelta = delta.content;
          }
          if (typeof delta.reasoning_content === "string" && delta.reasoning_content.length > 0) {
            chunk.reasoningDelta = delta.reasoning_content;
          }
          if (Array.isArray(delta.tool_calls) && delta.tool_calls.length > 0) {
            const tc = delta.tool_calls[0];
            chunk.toolCallDelta = {
              index: tc.index ?? 0,
              id: tc.id,
              name: tc.function?.name,
              argumentsDelta: tc.function?.arguments,
            };
          }
          if (json.usage) {
            chunk.usage = Usage.fromApi(json.usage);
          }
          queue.push(chunk);
        } catch {
          /* skip malformed sse frame */
        }
      },
    });

    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    try {
      while (true) {
        if (queue.length > 0) {
          yield queue.shift()!;
          continue;
        }
        if (done) break;
        const { value, done: streamDone } = await reader.read();
        if (streamDone) break;
        parser.feed(decoder.decode(value, { stream: true }));
      }
      while (queue.length > 0) yield queue.shift()!;
    } finally {
      clearTimeout(timer);
      reader.releaseLock();
    }
  }
}

export type { ChatMessage, ToolCall, ToolSpec };
