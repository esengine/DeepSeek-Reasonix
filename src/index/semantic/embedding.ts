const DEFAULT_OLLAMA_URL = "http://localhost:11434";
const DEFAULT_EMBED_MODEL = "nomic-embed-text";
const DEFAULT_TIMEOUT_MS = 30_000;

export type EmbedOptions =
  | {
      provider?: "ollama";
      baseUrl?: string;
      model?: string;
      timeoutMs?: number;
      signal?: AbortSignal;
    }
  | {
      provider: "openai-compat";
      baseUrl: string;
      apiKey: string;
      model: string;
      extraBody?: Record<string, unknown>;
      timeoutMs?: number;
      signal?: AbortSignal;
    };

export class EmbeddingError extends Error {
  constructor(
    message: string,
    public override readonly cause?: unknown,
  ) {
    super(message);
    this.name = "EmbeddingError";
  }
}

export async function embed(text: string, opts: EmbedOptions = {}): Promise<Float32Array> {
  if (opts.provider === "openai-compat") return await embedOpenAICompat(text, opts);
  return await embedOllama(text, opts);
}

export async function embedAll(
  texts: readonly string[],
  opts: EmbedOptions & {
    onProgress?: (done: number, total: number) => void;
    onError?: (index: number, err: unknown) => void;
  } = {},
): Promise<Array<Float32Array | null>> {
  if (opts.provider === "openai-compat") return await embedAllOpenAICompat(texts, opts);
  const out: Array<Float32Array | null> = [];
  for (let i = 0; i < texts.length; i++) {
    if (opts.signal?.aborted) throw new EmbeddingError("embedding aborted");
    const text = texts[i];
    if (text === undefined) continue;
    try {
      out.push(await embed(text, opts));
    } catch (err) {
      if (isAbortError(err) || opts.signal?.aborted) {
        throw new EmbeddingError("embedding aborted", err);
      }
      opts.onError?.(i, err);
      out.push(null);
    }
    opts.onProgress?.(i + 1, texts.length);
  }
  return out;
}

export async function probeOllama(
  opts: { baseUrl?: string; signal?: AbortSignal } = {},
): Promise<{ ok: true; models: string[] } | { ok: false; error: string }> {
  const baseUrl = opts.baseUrl ?? process.env.OLLAMA_URL ?? DEFAULT_OLLAMA_URL;
  try {
    const res = await fetch(`${baseUrl}/api/tags`, { signal: opts.signal });
    if (!res.ok) return { ok: false, error: `Ollama returned ${res.status}` };
    const json = (await res.json()) as { models?: Array<{ name?: string }> };
    const models = (json.models ?? [])
      .map((m) => m.name)
      .filter((n): n is string => typeof n === "string");
    return { ok: true, models };
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    return { ok: false, error: msg };
  }
}

async function embedOllama(
  text: string,
  opts: Extract<EmbedOptions, { provider?: "ollama" }>,
): Promise<Float32Array> {
  const baseUrl = opts.baseUrl ?? process.env.OLLAMA_URL ?? DEFAULT_OLLAMA_URL;
  const model = opts.model ?? process.env.REASONIX_EMBED_MODEL ?? DEFAULT_EMBED_MODEL;
  const timeoutMs = opts.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const { controller, cleanup } = composeAbort(opts.signal, timeoutMs, "embedding timeout");

  let res: Response;
  try {
    res = await fetch(`${baseUrl}/api/embeddings`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ model, prompt: text }),
      signal: controller.signal,
    });
  } catch (err) {
    cleanup();
    const msg = err instanceof Error ? err.message : String(err);
    if (/ECONNREFUSED|connect ECONNREFUSED|fetch failed/i.test(msg)) {
      throw new EmbeddingError(
        `Cannot reach Ollama at ${baseUrl}. Install from https://ollama.com, then run \`ollama pull ${model}\` and \`ollama serve\`. Override the URL via OLLAMA_URL.`,
        err,
      );
    }
    throw new EmbeddingError(`embedding request failed: ${msg}`, err);
  } finally {
    cleanup();
  }

  if (!res.ok) {
    const body = await res.text().catch(() => "");
    if (res.status === 404 && /model.*not found/i.test(body)) {
      throw new EmbeddingError(
        `Embedding model "${model}" not pulled. Run \`ollama pull ${model}\` once, then retry.`,
      );
    }
    throw new EmbeddingError(`Ollama returned ${res.status}: ${body.slice(0, 200)}`);
  }

  const json = (await res.json()) as { embedding?: unknown };
  if (!json.embedding || !Array.isArray(json.embedding)) {
    throw new EmbeddingError("Ollama response missing 'embedding' array");
  }
  return toFloat32Array(json.embedding, "embedding");
}

async function embedOpenAICompat(
  text: string,
  opts: Extract<EmbedOptions, { provider: "openai-compat" }>,
): Promise<Float32Array> {
  const vectors = await requestOpenAICompatEmbeddings(text, opts);
  return vectors[0] ?? new Float32Array(0);
}

async function embedAllOpenAICompat(
  texts: readonly string[],
  opts: Extract<EmbedOptions, { provider: "openai-compat" }> & {
    onProgress?: (done: number, total: number) => void;
    onError?: (index: number, err: unknown) => void;
  },
): Promise<Array<Float32Array | null>> {
  if (texts.length === 0) return [];
  if (opts.signal?.aborted) throw new EmbeddingError("embedding aborted");
  const vectors = await requestOpenAICompatEmbeddings([...texts], opts);
  opts.onProgress?.(texts.length, texts.length);
  return vectors;
}

async function requestOpenAICompatEmbeddings(
  input: string | string[],
  opts: Extract<EmbedOptions, { provider: "openai-compat" }>,
): Promise<Float32Array[]> {
  const timeoutMs = opts.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const { controller, cleanup } = composeAbort(opts.signal, timeoutMs, "embedding timeout");
  const url = opts.baseUrl.trim();
  const body = {
    ...(opts.extraBody ?? {}),
    model: opts.model,
    input,
    encoding_format: "float",
  };

  let res: Response;
  try {
    res = await fetch(url, {
      method: "POST",
      headers: {
        authorization: `Bearer ${opts.apiKey}`,
        "content-type": "application/json",
      },
      body: JSON.stringify(body),
      signal: controller.signal,
    });
  } catch (err) {
    cleanup();
    if (isAbortError(err) || opts.signal?.aborted) {
      throw new EmbeddingError("embedding aborted", err);
    }
    const msg = err instanceof Error ? err.message : String(err);
    throw new EmbeddingError(`Cannot reach OpenAI-compatible embeddings at ${url}: ${msg}`, err);
  } finally {
    cleanup();
  }

  if (!res.ok) {
    const raw = await res.text().catch(() => "");
    const bodyText = raw.slice(0, 300);
    if (res.status === 401 || res.status === 403) {
      throw new EmbeddingError(
        `OpenAI-compatible API rejected the API key for ${url}. Response ${res.status}: ${bodyText}`,
      );
    }
    if (res.status === 404) {
      throw new EmbeddingError(
        `Embeddings endpoint not found at ${url}. Check the configured API URL. Response ${res.status}: ${bodyText}`,
      );
    }
    if (res.status === 400) {
      throw new EmbeddingError(
        `Embedding provider returned 400: ${bodyText}. Check model and custom request body fields.`,
      );
    }
    throw new EmbeddingError(`OpenAI-compatible API returned ${res.status}: ${bodyText}`);
  }

  const json = (await res.json()) as {
    data?: Array<{ index?: unknown; embedding?: unknown }>;
  };
  if (!Array.isArray(json.data)) {
    throw new EmbeddingError("OpenAI-compatible response missing 'data' array");
  }
  const size = Array.isArray(input) ? input.length : 1;
  const out: Array<Float32Array | null> = new Array(size).fill(null);
  for (const row of json.data) {
    const rawIndex = row.index;
    if (
      typeof rawIndex !== "number" ||
      !Number.isInteger(rawIndex) ||
      rawIndex < 0 ||
      rawIndex >= size
    ) {
      throw new EmbeddingError("OpenAI-compatible response returned an invalid embedding index");
    }
    const index = rawIndex;
    if (!Array.isArray(row.embedding)) {
      throw new EmbeddingError(`OpenAI-compatible response missing embedding for index ${index}`);
    }
    out[index] = toFloat32Array(row.embedding, `data[${index}].embedding`);
  }
  for (let i = 0; i < out.length; i++) {
    if (!out[i])
      throw new EmbeddingError(`OpenAI-compatible response missing embedding at index ${i}`);
  }
  return out as Float32Array[];
}

function toFloat32Array(values: unknown[], label: string): Float32Array {
  const out = new Float32Array(values.length);
  for (let i = 0; i < values.length; i++) {
    const value = values[i];
    if (typeof value !== "number" || !Number.isFinite(value)) {
      throw new EmbeddingError(`${label}[${i}] is not a finite number`);
    }
    out[i] = value;
  }
  return out;
}

function composeAbort(
  signal: AbortSignal | undefined,
  timeoutMs: number,
  reason: string,
): { controller: AbortController; cleanup: () => void } {
  const controller = new AbortController();
  const onCallerAbort = () => controller.abort(signal?.reason);
  if (signal) {
    if (signal.aborted) controller.abort(signal.reason);
    else signal.addEventListener("abort", onCallerAbort, { once: true });
  }
  const timer = setTimeout(() => controller.abort(new Error(reason)), timeoutMs);
  return {
    controller,
    cleanup: () => {
      clearTimeout(timer);
      if (signal) signal.removeEventListener("abort", onCallerAbort);
    },
  };
}

function isAbortError(err: unknown): boolean {
  if (err instanceof Error) {
    if (err.name === "AbortError") return true;
    if (/aborted/i.test(err.message)) return true;
  }
  return false;
}
