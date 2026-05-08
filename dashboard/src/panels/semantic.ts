import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { t, useLang } from "../i18n/index.js";
import { api } from "../lib/api.js";
import { fmtBytes, fmtNum, fmtRelativeTime } from "../lib/format.js";
import { html } from "../lib/html.js";

interface SemanticConfigView {
  provider: "ollama" | "openai-compat";
  ollama: {
    baseUrl: string;
    model: string;
  };
  openaiCompat: {
    baseUrl: string;
    apiKey: string;
    apiKeySet: boolean;
    model: string;
    extraBody: Record<string, unknown>;
  };
}

interface SemanticData {
  attached?: boolean;
  reason?: string;
  root?: string;
  provider?: "ollama" | "openai-compat";
  providerConfig?: SemanticConfigView;
  providerStatus?:
    | {
        kind: "ollama";
        ready: boolean;
        baseUrl: string;
        binaryFound?: boolean;
        daemonRunning?: boolean;
        modelPulled?: boolean;
        modelName?: string;
        installedModels?: string[];
        error?: string;
      }
    | {
        kind: "openai-compat";
        ready: boolean;
        baseUrl: string;
        apiKeySet: boolean;
        model: string;
        extraBodyKeys: string[];
      };
  index?: IndexInfo;
  job?: SemanticJob | null;
  pull?: { status: string; startedAt: number; lastLine?: string } | null;
  ollama?: {
    binaryFound?: boolean;
    daemonRunning?: boolean;
    modelPulled?: boolean;
    modelName?: string;
    installedModels?: string[];
    error?: string;
  };
}

interface IndexInfo {
  exists: boolean;
  provider?: "ollama" | "openai-compat";
  chunks?: number;
  files?: number;
  dim?: number;
  sizeBytes?: number;
  lastBuiltMs?: number;
  model?: string;
  builtWith?: { provider: "ollama" | "openai-compat"; model: string };
  current?: { provider: "ollama" | "openai-compat"; model: string };
  compatible?: boolean;
  mismatch?: "provider" | "model" | null;
}

interface SemanticJob {
  phase: string;
  startedAt: number;
  finishedAt?: number | null;
  cancelledAt?: number | null;
  lastPhase?: string | null;
  chunksTotal?: number;
  chunksDone?: number;
  filesScanned?: number;
  filesChanged?: number;
  filesSkipped?: number;
  aborted?: boolean;
  error?: string;
  result?: {
    chunksAdded: number;
    chunksRemoved: number;
    chunksSkipped?: number;
    durationMs: number;
    skipBuckets?: Record<string, number>;
  };
}

interface SemanticConfigDraft {
  provider: "ollama" | "openai-compat";
  ollama: {
    baseUrl: string;
    model: string;
  };
  openaiCompat: {
    baseUrl: string;
    apiKey: string;
    model: string;
    extraBodyText: string;
    apiKeySet: boolean;
  };
}

export interface SemanticDraftValidation {
  extraBody: Record<string, unknown>;
  error: string | null;
}

export function SemanticPanel() {
  useLang();
  const [data, setData] = useState<SemanticData | null>(null);
  const [draft, setDraft] = useState<SemanticConfigDraft | null>(null);
  const [draftDirty, setDraftDirty] = useState(false);
  const draftDirtyRef = useRef(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [info, setInfo] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const [semantic, config] = await Promise.all([
        api<SemanticData>("/semantic"),
        api<SemanticConfigView>("/semantic/config"),
      ]);
      setData(semantic);
      setDraft((current) => (current && draftDirtyRef.current ? current : toConfigDraft(config)));
    } catch (err) {
      setError((err as Error).message);
    }
  }, []);

  useEffect(() => {
    load();
    const phase = data?.job?.phase;
    const running = isActiveSemanticPhase(phase);
    const pulling = data?.pull?.status === "pulling";
    const ms = running || pulling ? 1200 : 5000;
    const id = setInterval(load, ms);
    return () => clearInterval(id);
  }, [load, data?.job?.phase, data?.pull?.status]);

  const start = useCallback(
    async (rebuild: boolean) => {
      if (!draft) return;
      setBusy(true);
      setError(null);
      setInfo(null);
      try {
        const validation = validateSemanticDraft(draft);
        if (draftDirty) {
          throw new Error(t("semantic.saveBeforeIndex"));
        }
        if (validation.error) {
          throw new Error(validation.error);
        }
        await api("/semantic/start", { method: "POST", body: { rebuild: !!rebuild } });
        setInfo(rebuild ? t("semantic.rebuildStarted") : t("semantic.incrementalStarted"));
        await load();
      } catch (err) {
        setError((err as Error).message);
      } finally {
        setBusy(false);
      }
    },
    [draft, draftDirty, load],
  );

  const stop = useCallback(async () => {
    setBusy(true);
    setError(null);
    try {
      await api("/semantic/stop", { method: "POST", body: {} });
      setInfo(t("semantic.stopRequested"));
      await load();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }, [load]);

  const startDaemon = useCallback(async () => {
    setBusy(true);
    setError(null);
    setInfo(t("semantic.startingDaemon"));
    try {
      const r = await api<{ ready: boolean }>("/semantic/ollama/start", {
        method: "POST",
        body: {},
      });
      setInfo(r.ready ? t("semantic.daemonUp") : t("semantic.daemonTimeout"));
      await load();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }, [load]);

  const pullModel = useCallback(
    async (model: string) => {
      setBusy(true);
      setError(null);
      setInfo(t("semantic.pullingModel", { model }));
      try {
        await api("/semantic/ollama/pull", { method: "POST", body: { model } });
        await load();
      } catch (err) {
        setError((err as Error).message);
      } finally {
        setBusy(false);
      }
    },
    [load],
  );

  const saveProviderConfig = useCallback(async () => {
    if (!draft) return;
    setBusy(true);
    setError(null);
    setInfo(null);
    try {
      const extraBody = semanticValidation.extraBody;
      await api("/semantic/config", {
        method: "POST",
        body: {
          provider: draft.provider,
          ollama: {
            baseUrl: draft.ollama.baseUrl,
            model: draft.ollama.model,
          },
          openaiCompat: {
            baseUrl: draft.openaiCompat.baseUrl,
            apiKey: draft.openaiCompat.apiKey,
            model: draft.openaiCompat.model,
            extraBody,
          },
        },
      });
      setDraftDirty(false);
      draftDirtyRef.current = false;
      setInfo(t("semantic.savedConfig", { count: 1 }));
      await load();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }, [draft, load]);

  if (!data && !error) {
    return html`<div class="card" style="color:var(--fg-3)">${t("common.loading")}</div>`;
  }
  if (error && !data) return html`<div class="card accent-err">${error}</div>`;
  if (!data || !draft) return null;

  if (!data.attached) {
    return html`
      <div class="card" style="color:var(--fg-3)">
        <div class="card-h"><span class="title">${t("semantic.codeRequired")}</span></div>
        <div class="card-b">${data.reason}</div>
      </div>
    `;
  }

  const job = data.job;
  const phase = job?.phase;
  const running = isActiveSemanticPhase(phase);
  const pull = data.pull;
  const pulling = pull?.status === "pulling";
  const provider = data.providerStatus?.kind ?? draft.provider;
  const ready = data.providerStatus?.ready === true;
  const isOllama = provider === "ollama";
  const ollama = data.providerStatus?.kind === "ollama" ? data.providerStatus : null;
  const remote = data.providerStatus?.kind === "openai-compat" ? data.providerStatus : null;
  const binaryFound = ollama?.binaryFound === true;
  const daemonRunning = ollama?.daemonRunning === true;
  const modelPulled = ollama?.modelPulled === true;
  const modelName = isOllama
    ? (ollama?.modelName ?? draft.ollama.model ?? "nomic-embed-text")
    : draft.openaiCompat.model;

  const sectionH3 = (text: string) => html`
    <h3 style="margin:18px 0 8px;font-family:var(--font-mono);font-size:11px;color:var(--fg-3);text-transform:uppercase;letter-spacing:.1em">${text}</h3>
  `;

  const idx = data.index;
  const indexReady = idx?.exists === true && idx.compatible !== false;
  const indexMismatch = idx?.exists === true && idx.compatible === false;
  const semanticValidation = validateSemanticDraft(draft);
  const semanticDraftBlocked = draftDirty || semanticValidation.error !== null;
  return html`
    <div style="display:grid;grid-template-columns:minmax(0,1fr) 280px;gap:14px;align-items:start">
      <div style="display:flex;flex-direction:column;gap:10px;min-width:0">
        <div class="chips">
          <span class=${`chip-f static ${indexReady ? "active" : ""}`}>
            ${indexReady ? t("semantic.indexBuilt") : t("semantic.noIndex")}
          </span>
          ${
            ready
              ? html`<span class="chip-f static" style="border-color:var(--c-ok);color:var(--c-ok)">${t("semantic.ready")}</span>`
              : html`<span class="chip-f static" style="border-color:var(--c-warn);color:var(--c-warn)">${t("semantic.setupNeeded")}</span>`
          }
        </div>
        ${error ? html`<div class="card accent-err">${error}</div>` : null}

        <div class="card">
          <div class="card-h"><span class="title">${t("semantic.provider")}</span></div>
          <div class="form-row">
            <span class="lbl">${t("semantic.providerType")}</span>
            <select
              class="input mono"
              value=${draft.provider}
              onInput=${(e: Event) => {
                draftDirtyRef.current = true;
                setDraftDirty(true);
                setDraft({
                  ...draft,
                  provider: (e.target as HTMLSelectElement).value as "ollama" | "openai-compat",
                });
              }}
            >
              <option value="ollama">Ollama</option>
              <option value="openai-compat">OpenAI-Compatible</option>
            </select>
          </div>
          ${
            draft.provider === "ollama"
              ? html`
                <div class="form-row">
                  <span class="lbl">${t("semantic.model")}</span>
                  <input
                    class="input mono"
                    type="text"
                    value=${draft.ollama.model}
                    onInput=${(e: Event) => {
                      draftDirtyRef.current = true;
                      setDraftDirty(true);
                      setDraft({
                        ...draft,
                        ollama: { ...draft.ollama, model: (e.target as HTMLInputElement).value },
                      });
                    }}
                  />
                </div>
              `
              : html`
                <div class="form-row">
                  <span class="lbl">${t("semantic.apiUrl")}</span>
                  <input
                    class="input mono"
                    type="text"
                    placeholder="https://api.openai.com/v1/embeddings"
                    value=${draft.openaiCompat.baseUrl}
                    onInput=${(e: Event) => {
                      draftDirtyRef.current = true;
                      setDraftDirty(true);
                      setDraft({
                        ...draft,
                        openaiCompat: {
                          ...draft.openaiCompat,
                          baseUrl: (e.target as HTMLInputElement).value,
                        },
                      });
                    }}
                  />
                </div>
                <div class="form-row">
                  <span class="lbl">${t("semantic.apiKey")}</span>
                  <input
                    class="input mono"
                    type="password"
                    placeholder=${draft.openaiCompat.apiKeySet ? t("semantic.keepExistingKey") : "sk-..."}
                    value=${draft.openaiCompat.apiKey}
                    onInput=${(e: Event) => {
                      draftDirtyRef.current = true;
                      setDraftDirty(true);
                      setDraft({
                        ...draft,
                        openaiCompat: {
                          ...draft.openaiCompat,
                          apiKey: (e.target as HTMLInputElement).value,
                        },
                      });
                    }}
                  />
                  <div style="color:var(--fg-3);font-size:12px">${t("semantic.apiKeyStoredNote")}</div>
                </div>
                <div class="form-row">
                  <span class="lbl">${t("semantic.model")}</span>
                  <input
                    class="input mono"
                    type="text"
                    value=${draft.openaiCompat.model}
                    onInput=${(e: Event) => {
                      draftDirtyRef.current = true;
                      setDraftDirty(true);
                      setDraft({
                        ...draft,
                        openaiCompat: {
                          ...draft.openaiCompat,
                          model: (e.target as HTMLInputElement).value,
                        },
                      });
                    }}
                  />
                </div>
                <details style="margin-top:10px">
                  <summary style="cursor:pointer;color:var(--fg-2);font-size:12px">${t("semantic.customRequestBody")}</summary>
                  <div class="form-row" style="margin-top:10px">
                    <span class="lbl">${t("semantic.customRequestBody")}</span>
                    <textarea
                      class="input mono"
                      rows="6"
                      value=${draft.openaiCompat.extraBodyText}
                      onInput=${(e: Event) => {
                        draftDirtyRef.current = true;
                        setDraftDirty(true);
                        setDraft({
                          ...draft,
                          openaiCompat: {
                            ...draft.openaiCompat,
                            extraBodyText: (e.target as HTMLTextAreaElement).value,
                          },
                        });
                      }}
                    ></textarea>
                  </div>
                </details>
                ${
                  semanticValidation.error
                    ? html`<div style="color:var(--c-err);font-size:12px;margin-top:-2px">${semanticValidation.error}</div>`
                    : null
                }
              `
          }
          <div style="display:flex;gap:6px;margin-top:10px">
            <button class="btn primary" disabled=${busy || semanticValidation.error !== null} onClick=${saveProviderConfig}>${t("common.save")}</button>
          </div>
        </div>
        ${info ? html`<div><span class="pill info">${info}</span></div>` : null}

        ${indexReady ? html`<${SemanticSearchSection} />` : null}

        ${
          isOllama && !binaryFound
            ? html`
              <div class="card">
                <div class="card-h"><span class="title">${t("semantic.installOllama")}</span></div>
                <div class="card-b" style="font-size:13px">
                  ${t("semantic.installOllamaDesc")}
                  <ul style="margin:10px 0 4px 18px;padding:0">
                    <li><strong>${t("semantic.macWindows")}</strong> ${t("semantic.download")} <a href="https://ollama.com/download" target="_blank" rel="noreferrer">ollama.com/download</a></li>
                    <li><strong>${t("semantic.linux")}</strong> <code class="mono">curl -fsSL https://ollama.com/install.sh | sh</code></li>
                  </ul>
                  <div style="color:var(--fg-3);margin-top:8px">${t("semantic.refreshHint", { model: modelName })}</div>
                </div>
              </div>
            `
            : null
        }
        ${
          isOllama && binaryFound && !daemonRunning
            ? html`
              <div class="card">
                <div class="card-h"><span class="title">${t("semantic.daemon")}</span></div>
                <div class="card-b" style="font-size:13px">
                  ${t("semantic.daemonDesc")}
                  <div style="display:flex;gap:8px;margin-top:10px;align-items:center">
                    <button class="primary" disabled=${busy} onClick=${startDaemon}>${t("semantic.startDaemon")}</button>
                    <span style="color:var(--fg-3);font-size:12px">${t("semantic.runsOllama")}</span>
                  </div>
                </div>
              </div>
            `
            : null
        }
        ${
          isOllama && daemonRunning && !modelPulled
            ? html`
              <div class="card">
                <div class="card-h"><span class="title">${t("semantic.model")}</span></div>
                <div class="card-b" style="font-size:13px">
                  ${t("semantic.modelMissing", { model: modelName })}${pulling ? "" : ` ${t("semantic.modelSize")}`}
                  <div style="display:flex;gap:8px;margin-top:10px">
                    <button class="primary" disabled=${busy || pulling} onClick=${() => pullModel(modelName)}>
                      ${pulling ? t("semantic.pulling") : t("semantic.pullModel", { model: modelName })}
                    </button>
                  </div>
                  ${
                    pull
                      ? html`
                        <div style="margin-top:10px;display:flex;gap:10px;align-items:center;font-size:11.5px">
                          <span class=${`pill ${pull.status === "done" ? "ok" : pull.status === "error" ? "err" : ""}`}>${pull.status}</span>
                          <span style="color:var(--fg-3)">${((Date.now() - pull.startedAt) / 1000).toFixed(1)}s</span>
                          ${pull.lastLine ? html`<code class="mono" style="color:var(--fg-3)">${pull.lastLine}</code>` : null}
                        </div>
                      `
                      : null
                  }
                </div>
              </div>
            `
            : null
        }
        ${
          !isOllama
            ? html`
              <div class="card">
                <div class="card-h"><span class="title">${t("semantic.remoteProvider")}</span></div>
                <div class="card-b" style="font-size:13px;color:var(--fg-2)">
                  ${t("semantic.remoteProviderDesc")}
                </div>
              </div>
            `
            : null
        }

        ${
          job
            ? html`
              ${sectionH3(t("semantic.job"))}
              <${SemanticJobView} job=${job} running=${running} />
            `
            : null
        }
      </div>

      <aside style="display:flex;flex-direction:column;gap:10px">
        <div class="card">
          <div class="card-h">
            <span class="title">${t("semantic.indexStatus")}</span>
            <span class="meta">
              ${
                idx?.exists
                  ? idx.compatible === false
                    ? html`<span class="pill warn">${t("semantic.incompatibleStatus")}</span>`
                    : html`<span class="pill ok">${t("semantic.builtStatus")}</span>`
                  : html`<span class="pill">${t("system.none")}</span>`
              }
            </span>
          </div>
          ${
            idx?.exists
              ? html`
                <div class="rail-kv"><span class="k">${t("semantic.provider")}</span><span class="v">${idx.builtWith?.provider ?? idx.provider ?? provider}</span></div>
                <div class="rail-kv"><span class="k">${t("semantic.chunks")}</span><span class="v">${fmtNum(idx.chunks)}</span></div>
                <div class="rail-kv"><span class="k">${t("semantic.files")}</span><span class="v">${fmtNum(idx.files)}</span></div>
                <div class="rail-kv"><span class="k">${t("semantic.model")}</span><span class="v" style="font-size:11px">${idx.builtWith?.model ?? idx.model ?? modelName}</span></div>
                <div class="rail-kv"><span class="k">${t("semantic.dim")}</span><span class="v">${fmtNum(idx.dim)}</span></div>
                <div class="rail-kv"><span class="k">${t("semantic.size")}</span><span class="v">${fmtBytes(idx.sizeBytes)}</span></div>
                <div class="rail-kv"><span class="k">${t("semantic.lastBuild")}</span><span class="v">${fmtRelativeTime(idx.lastBuiltMs ?? null)}</span></div>
                ${
                  idx.compatible === false
                    ? html`
                      <div class="rail-kv"><span class="k">${t("semantic.builtWith")}</span><span class="v" style="font-size:11px">${idx.builtWith?.provider} · ${idx.builtWith?.model}</span></div>
                      <div class="rail-kv"><span class="k">${t("semantic.currentTarget")}</span><span class="v" style="font-size:11px">${idx.current?.provider} · ${idx.current?.model}</span></div>
                      <div style="color:var(--c-warn);font-size:12px;padding-top:8px">${t("semantic.incompatibleHint")}</div>
                    `
                    : null
                }
              `
              : html`<div style="color:var(--fg-3);font-size:12.5px;padding:6px 0">${t("semantic.runIndexHint")}</div>`
          }
          <div style="display:flex;gap:6px;margin-top:10px;flex-wrap:wrap">
            <button class="primary" disabled=${busy || running || !ready || semanticDraftBlocked} onClick=${() => start(false)}>${indexReady ? t("semantic.reIndex") : t("semantic.build")}</button>
            ${
              idx?.exists
                ? html`<button disabled=${busy || running || !ready || semanticDraftBlocked} onClick=${() => start(true)}>${t("semantic.rebuild")}</button>`
                : null
            }
            ${
              running
                ? html`<button onClick=${stop} style="border-color:var(--c-err);color:var(--c-err)">${t("semantic.stop")}</button>`
                : null
            }
          </div>
        </div>

        <div class="card">
          <div class="card-h"><span class="title">${isOllama ? t("semantic.ollama") : t("semantic.openaiCompat")}</span></div>
          ${
            isOllama
              ? html`
                <div class="rail-kv"><span class="k">${t("semantic.binary")}</span><span class="v">${binaryFound ? html`<span class="pill ok">${t("semantic.found")}</span>` : html`<span class="pill err">${t("semantic.missing")}</span>`}</span></div>
                <div class="rail-kv"><span class="k">${t("semantic.daemonStatus")}</span><span class="v">${daemonRunning ? html`<span class="pill ok">${t("semantic.up")}</span>` : html`<span class="pill warn">${t("semantic.down")}</span>`}</span></div>
                <div class="rail-kv"><span class="k">${t("semantic.model")}</span><span class="v">${modelPulled ? html`<span class="pill ok">${t("semantic.pulled")}</span>` : html`<span class="pill warn">${t("semantic.missing")}</span>`}</span></div>
              `
              : html`
                <div class="rail-kv"><span class="k">${t("semantic.apiUrl")}</span><span class="v" style="font-size:11px;max-width:160px;overflow-wrap:anywhere;word-break:break-word;text-align:right">${remote?.baseUrl ?? draft.openaiCompat.baseUrl}</span></div>
                <div class="rail-kv"><span class="k">${t("semantic.apiKey")}</span><span class="v">${remote?.apiKeySet ? html`<span class="pill ok">${t("semantic.found")}</span>` : html`<span class="pill warn">${t("semantic.missing")}</span>`}</span></div>
                <div class="rail-kv"><span class="k">${t("semantic.model")}</span><span class="v" style="font-size:11px">${remote?.model ?? draft.openaiCompat.model}</span></div>
                <div class="rail-kv"><span class="k">${t("semantic.extraBody")}</span><span class="v">${fmtNum(remote?.extraBodyKeys.length ?? 0)}</span></div>
              `
          }
        </div>

        <${SemanticExcludesCard} />
      </aside>
    </div>
  `;
}

function toConfigDraft(config: SemanticConfigView): SemanticConfigDraft {
  return {
    provider: config.provider,
    ollama: {
      baseUrl: config.ollama.baseUrl,
      model: config.ollama.model,
    },
    openaiCompat: {
      baseUrl: config.openaiCompat.baseUrl,
      apiKey: "",
      model: config.openaiCompat.model,
      extraBodyText: JSON.stringify(config.openaiCompat.extraBody ?? {}, null, 2),
      apiKeySet: config.openaiCompat.apiKeySet,
    },
  };
}

export function validateSemanticDraft(draft: SemanticConfigDraft): SemanticDraftValidation {
  if (draft.provider !== "openai-compat") {
    return { extraBody: {}, error: null };
  }
  const raw = draft.openaiCompat.extraBodyText.trim();
  if (!raw) {
    return { extraBody: {}, error: null };
  }
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch (err) {
    return {
      extraBody: {},
      error: t("semantic.invalidCustomRequestBody", { error: (err as Error).message }),
    };
  }
  if (!isPlainObject(parsed)) {
    return { extraBody: {}, error: t("semantic.customRequestBodyMustBeObject") };
  }
  return { extraBody: parsed, error: null };
}

interface IndexConfig {
  excludeDirs?: string[];
  excludeFiles?: string[];
  excludeExts?: string[];
  excludePatterns?: string[];
  respectGitignore?: boolean;
  maxFileBytes?: number;
}

interface IndexConfigResponse {
  resolved: IndexConfig;
  defaults: IndexConfig;
}

interface ExcludeDraft {
  excludeDirs: string[];
  excludeFiles: string[];
  excludeExts: string[];
  excludePatterns: string[];
  respectGitignore: boolean;
  maxFileBytes: number;
}

interface PreviewData {
  filesIncluded: number;
  skipBuckets?: Record<string, number>;
  skipSamples?: Record<string, string[]>;
  sampleIncluded?: string[];
}

interface SearchHit {
  path: string;
  startLine: number;
  endLine: number;
  score: number;
  snippet: string;
}

interface SearchResponse {
  hits: SearchHit[];
  elapsedMs: number;
  provider?: string;
  model: string;
}

function SemanticSearchSection() {
  useLang();
  const [query, setQuery] = useState("");
  const [hits, setHits] = useState<SearchHit[] | null>(null);
  const [meta, setMeta] = useState<{ elapsedMs: number; model: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const runSearch = useCallback(async () => {
    const q = query.trim();
    if (!q || busy) return;
    setBusy(true);
    setError(null);
    try {
      const r = await api<SearchResponse>("/semantic/search", {
        method: "POST",
        body: { query: q, topK: 8, minScore: 0.3 },
      });
      setHits(r.hits);
      setMeta({ elapsedMs: r.elapsedMs, model: r.model });
    } catch (err) {
      setError((err as Error).message);
      setHits(null);
    } finally {
      setBusy(false);
    }
  }, [query, busy]);

  return html`
    <div style="margin-bottom:14px">
      <div style="position:relative">
        <div style="position:absolute;left:14px;top:50%;transform:translateY(-50%);color:var(--c-brand);font-family:var(--font-mono);font-size:14px;pointer-events:none">≈</div>
        <input
          type="text"
          class="mono"
          style="width:100%;padding:10px 14px 10px 38px;font-size:13.5px;background:var(--bg-input);border:1px solid var(--bd);border-radius:var(--r);color:var(--fg-0);outline:none"
          placeholder=${t("semantic.searchPlaceholder")}
          value=${query}
          disabled=${busy}
          onInput=${(e: Event) => setQuery((e.target as HTMLInputElement).value)}
          onKeyDown=${(e: KeyboardEvent) => {
            if (e.key === "Enter") {
              e.preventDefault();
              runSearch();
            }
          }}
        />
      </div>
      ${
        hits || busy || error
          ? html`
            <div style="font-family:var(--font-mono);font-size:11px;color:var(--fg-3);margin:8px 0 6px;display:flex;align-items:center;gap:8px">
              ${
                busy
                  ? html`<span>${t("semantic.searching")}</span>`
                  : error
                    ? html`<span style="color:var(--c-err)">${error}</span>`
                    : hits
                      ? html`<span>${t("semantic.results", { count: hits.length, s: hits.length === 1 ? "" : "s", ms: meta?.elapsedMs ?? 0, model: meta?.model ?? "" })}</span>`
                      : null
              }
            </div>
            ${
              hits && hits.length > 0
                ? html`
                  <div class="card" style="padding:0;max-height:420px;overflow-y:auto">
                    ${hits.map(
                      (h) => html`
                        <div class="sr-card">
                          <div class="sr-h">
                            <span class="sr-path">${h.path}</span>
                            <span class="sr-loc">L${h.startLine} – L${h.endLine}</span>
                            <span class="sr-score">${h.score.toFixed(3)}</span>
                          </div>
                          <div class="sr-snip">${truncateSnippet(h.snippet)}</div>
                        </div>
                      `,
                    )}
                  </div>
                `
                : hits && hits.length === 0 && !busy
                  ? html`<div class="card" style="color:var(--fg-3);font-size:12px">${t("semantic.noMatches")}</div>`
                  : null
            }
          `
          : null
      }
    </div>
  `;
}

function truncateSnippet(text: string, maxLines = 8): string {
  const lines = text.split("\n");
  if (lines.length <= maxLines) return text;
  return `${lines.slice(0, maxLines).join("\n")}\n  …(${lines.length - maxLines} more lines)`;
}

function toDraft(c: IndexConfig): ExcludeDraft {
  return {
    excludeDirs: c.excludeDirs ?? [],
    excludeFiles: c.excludeFiles ?? [],
    excludeExts: c.excludeExts ?? [],
    excludePatterns: c.excludePatterns ?? [],
    respectGitignore: c.respectGitignore !== false,
    maxFileBytes: c.maxFileBytes ?? 262144,
  };
}

function fromDraft(d: ExcludeDraft): IndexConfig {
  return {
    excludeDirs: d.excludeDirs,
    excludeFiles: d.excludeFiles,
    excludeExts: d.excludeExts,
    excludePatterns: d.excludePatterns,
    respectGitignore: !!d.respectGitignore,
    maxFileBytes: d.maxFileBytes,
  };
}

function SemanticExcludesCard() {
  useLang();
  const [data, setData] = useState<IndexConfigResponse | null>(null);
  const [draft, setDraft] = useState<ExcludeDraft | null>(null);
  const [preview, setPreview] = useState<PreviewData | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [info, setInfo] = useState<string | null>(null);

  const load = useCallback(async () => {
    try {
      const r = await api<IndexConfigResponse>("/index-config");
      setData(r);
      setDraft(toDraft(r.resolved));
    } catch (err) {
      setError((err as Error).message);
    }
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  const reset = useCallback(() => {
    if (data) setDraft(toDraft(data.defaults));
    setPreview(null);
  }, [data]);

  const save = useCallback(async () => {
    if (!draft) return;
    setBusy(true);
    setError(null);
    setInfo(null);
    try {
      const payload = fromDraft(draft);
      const r = await api<{ changed: string[] }>("/index-config", {
        method: "POST",
        body: payload,
      });
      setInfo(t("semantic.savedConfig", { count: r.changed.length || 0 }));
      await load();
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setBusy(false);
    }
  }, [draft, load]);

  const runPreview = useCallback(async () => {
    if (!draft) return;
    setBusy(true);
    setError(null);
    setInfo(t("semantic.runningPreview"));
    try {
      const payload = fromDraft(draft);
      const r = await api<PreviewData>("/index-config/preview", {
        method: "POST",
        body: payload,
      });
      setPreview(r);
      setInfo(null);
    } catch (err) {
      setError((err as Error).message);
      setInfo(null);
    } finally {
      setBusy(false);
    }
  }, [draft]);

  if (!draft) {
    return html`
      <div class="card">
        <div class="card-h"><span class="title">${t("semantic.indexConfig")}</span></div>
        <div style="color:var(--fg-3);font-size:12.5px">${t("common.loading")}</div>
      </div>
    `;
  }

  return html`
    <div class="card">
      <div class="card-h">
        <span class="title">${t("semantic.indexConfig")}</span>
        <span class="meta">
          <a class="mono" style="color:var(--c-brand);text-decoration:none;font-size:11px;cursor:pointer" onClick=${reset}>${t("semantic.reset")}</a>
        </span>
      </div>
      ${info ? html`<div style="margin-bottom:8px"><span class="pill ok">${info}</span></div>` : null}
      ${error ? html`<div class="card accent-err" style="margin-bottom:8px">${error}</div>` : null}

      <${ChipFormRow}
        label=${t("semantic.excludeDirs")}
        value=${draft.excludeDirs}
        onChange=${(v: string[]) => setDraft({ ...draft, excludeDirs: v })}
        placeholder="dist"
      />
      <${ChipFormRow}
        label=${t("semantic.excludeFiles")}
        value=${draft.excludeFiles}
        onChange=${(v: string[]) => setDraft({ ...draft, excludeFiles: v })}
        placeholder="package-lock.json"
      />
      <${ChipFormRow}
        label=${t("semantic.excludeExts")}
        value=${draft.excludeExts}
        onChange=${(v: string[]) => setDraft({ ...draft, excludeExts: v })}
        placeholder=".lock"
      />
      <${ChipFormRow}
        label=${t("semantic.excludePatterns")}
        sub=${t("semantic.glob")}
        value=${draft.excludePatterns}
        onChange=${(v: string[]) => setDraft({ ...draft, excludePatterns: v })}
        placeholder="**/*.test.ts"
      />

      <div class="checkbox-row" style="margin-top:8px;cursor:pointer" onClick=${() => setDraft({ ...draft, respectGitignore: !draft.respectGitignore })}>
        <span class=${`box ${draft.respectGitignore ? "on" : ""}`}>${draft.respectGitignore ? "✓" : ""}</span>
        <span>${t("semantic.respectGitignore")}</span>
      </div>

      <div class="form-row" style="margin-top:10px">
        <span class="lbl">${t("semantic.maxFileBytes")}</span>
        <input
          class="input mono"
          type="number"
          min="1024"
          step="1024"
          value=${draft.maxFileBytes}
          onInput=${(e: Event) => setDraft({ ...draft, maxFileBytes: Number((e.target as HTMLInputElement).value) || 0 })}
          style="font-size:12px"
        />
        <span class="help">${t("semantic.skipLarger", { size: (draft.maxFileBytes / 1024 / 1024).toFixed(1) })}</span>
      </div>

      <div style="display:flex;gap:6px;margin-top:10px">
        <button class="btn ghost" style="flex:1" disabled=${busy} onClick=${runPreview}><span class="g">⊕</span><span>${t("semantic.preview")}</span></button>
        <button class="btn primary" style="flex:1" disabled=${busy} onClick=${save}>${t("common.save")}</button>
      </div>

      ${preview ? html`<div style="margin-top:10px"><${ExcludesPreview} preview=${preview} /></div>` : null}
    </div>
  `;
}

function ExcludesPreview({ preview }: { preview: PreviewData }) {
  useLang();
  const buckets = preview.skipBuckets || {};
  const samples = preview.skipSamples || {};
  const totalSkipped = Object.values(buckets).reduce((a, b) => a + (b || 0), 0);
  const reasons = [
    "gitignore",
    "pattern",
    "defaultDir",
    "defaultFile",
    "binaryExt",
    "binaryContent",
    "tooLarge",
    "readError",
  ].filter((k) => (buckets[k] || 0) > 0);
  return html`
    <div class="excludes-preview">
      <div class="summary">${t("semantic.previewSummary", { included: preview.filesIncluded, skipped: totalSkipped })}</div>
      ${
        reasons.length === 0
          ? html`<div style="color:var(--fg-3)">${t("semantic.nothingSkipped")}</div>`
          : reasons.map(
              (r) => html`
              <details>
                <summary><strong>${r}: ${buckets[r]}</strong></summary>
                <ul>
                  ${(samples[r] || []).map((p) => html`<li><code>${p}</code></li>`)}
                  ${
                    (buckets[r] || 0) > (samples[r] || []).length
                      ? html`<li style="color:var(--fg-3)">…${(buckets[r] || 0) - (samples[r] || []).length} more</li>`
                      : null
                  }
                </ul>
              </details>
            `,
            )
      }
      ${
        preview.sampleIncluded?.length
          ? html`
            <details>
              <summary>${t("semantic.firstIncluded", { count: preview.sampleIncluded.length })}</summary>
              <ul>
                ${preview.sampleIncluded.map((p) => html`<li><code>${p}</code></li>`)}
              </ul>
            </details>
          `
          : null
      }
    </div>
  `;
}

function ChipFormRow({
  label,
  sub,
  value,
  onChange,
  placeholder = "+ add",
}: {
  label: string;
  sub?: string;
  value: string[];
  onChange: (v: string[]) => void;
  placeholder?: string;
}) {
  const [adding, setAdding] = useState("");
  const remove = (entry: string) => onChange(value.filter((v) => v !== entry));
  const commit = () => {
    const trimmed = adding.trim();
    if (!trimmed || value.includes(trimmed)) {
      setAdding("");
      return;
    }
    onChange([...value, trimmed]);
    setAdding("");
  };
  return html`
    <div class="form-row">
      <span class="lbl">${label}${sub ? html`<span style="color:var(--fg-3);font-weight:400;text-transform:none;letter-spacing:0"> · ${sub}</span>` : null}</span>
      <div style="display:flex;flex-wrap:wrap;gap:4px">
        ${value.map(
          (e) => html`
            <span class="chip-f static">
              <span>${e}</span>
              <span class="x" style="cursor:pointer" onClick=${() => remove(e)} title="remove">×</span>
            </span>
          `,
        )}
        <input
          type="text"
          class="chip-add-input"
          value=${adding}
          placeholder=${placeholder}
          onInput=${(ev: Event) => setAdding((ev.target as HTMLInputElement).value)}
          onKeyDown=${(ev: KeyboardEvent) => {
            if (ev.key === "Enter") {
              ev.preventDefault();
              commit();
            }
          }}
          onBlur=${commit}
        />
      </div>
    </div>
  `;
}

function SemanticJobView({ job, running }: { job: SemanticJob; running: boolean }) {
  useLang();
  const phaseLabel =
    (
      {
        setup: t("semantic.phaseSetup"),
        scan: t("semantic.phaseScan"),
        embed: t("semantic.phaseEmbed"),
        write: t("semantic.phaseWrite"),
        done: t("semantic.phaseDone"),
        error: t("semantic.phaseError"),
        cancelled: t("semantic.phaseCancelled"),
      } as Record<string, string>
    )[job.phase] ?? job.phase;
  const total = job.chunksTotal ?? 0;
  const doneN = job.chunksDone ?? 0;
  const ratio = total > 0 ? Math.min(1, doneN / total) : 0;
  const elapsedBase = job.finishedAt ?? Date.now();
  const elapsedSeconds = (elapsedBase - job.startedAt) / 1000;
  const elapsed = elapsedSeconds < 0.1 ? "<0.1s" : `${elapsedSeconds.toFixed(1)}s`;
  const phaseSummary =
    job.phase === "error" && job.lastPhase === "setup"
      ? t("semantic.setupFailed")
      : phaseLabel;

  return html`
    <div class="kv">
      <div><span class="kv-key">phase</span>
        <span class=${`pill ${job.phase === "error" ? "pill-err" : job.phase === "cancelled" ? "warn" : running ? "pill-active" : "pill-dim"}`}>${phaseSummary}</span>
        ${job.aborted && running ? html`<span class="pill warn" style="margin-left: 6px;">${t("semantic.stopping")}</span>` : null}
        <span style="color:var(--fg-3);margin-left:8px">${elapsed}</span>
      </div>
      ${
        job.filesScanned !== null && job.filesScanned !== undefined
          ? html`<div><span class="kv-key">${t("semantic.files")}</span>${t("semantic.scanned", { count: job.filesScanned })}${job.filesChanged != null ? ` · ${t("semantic.changed", { count: job.filesChanged })}` : ""}${job.filesSkipped ? ` · ${t("semantic.skipped", { count: job.filesSkipped })}` : ""}</div>`
          : null
      }
      ${
        total > 0
          ? html`
            <div>
              <span class="kv-key">${t("semantic.chunks")}</span>${t("semantic.chunksProgress", { done: doneN, total, pct: (ratio * 100).toFixed(0) })}
            </div>
            <div class="bar" style="margin-top: 4px;">
              <div class="fill" style=${`width: ${(ratio * 100).toFixed(1)}%; background: var(--primary);`}></div>
            </div>
          `
          : null
      }
      ${job.error ? html`<div><span class="kv-key">${t("semantic.phaseError")}</span><span class="err">${job.error}</span></div>` : null}
      ${
        job.result
          ? html`<div><span class="kv-key">${t("semantic.result")}</span>${t("semantic.added", { count: job.result.chunksAdded })} · ${t("semantic.removed", { count: job.result.chunksRemoved })}${job.result.chunksSkipped ? ` · ${t("semantic.failed", { count: job.result.chunksSkipped })}` : ""} · ${(job.result.durationMs / 1000).toFixed(1)}s</div>`
          : null
      }
      ${job.result?.skipBuckets ? html`<${SkipBucketsView} buckets=${job.result.skipBuckets} />` : null}
    </div>
  `;
}

function SkipBucketsView({ buckets }: { buckets: Record<string, number> }) {
  useLang();
  const order: [string, string][] = [
    ["gitignore", "gitignore"],
    ["pattern", "pattern"],
    ["defaultDir", "defaultDir"],
    ["defaultFile", "defaultFile"],
    ["binaryExt", "binaryExt"],
    ["binaryContent", "binaryContent"],
    ["tooLarge", "tooLarge"],
    ["readError", "readError"],
  ];
  const total = order.reduce((a, [k]) => a + (buckets[k] || 0), 0);
  if (total === 0) return null;
  const parts = order
    .filter(([k]) => (buckets[k] || 0) > 0)
    .map(([k, label]) => `${label}: ${buckets[k]}`);
  return html`<div><span class="kv-key">${t("semantic.skipped")}</span>${t("semantic.skippedFiles", { total, details: parts.join(", ") })}</div>`;
}

function isActiveSemanticPhase(phase: string | undefined): boolean {
  return phase === "setup" || phase === "scan" || phase === "embed" || phase === "write";
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return false;
  const proto = Object.getPrototypeOf(value);
  return proto === Object.prototype || proto === null;
}
