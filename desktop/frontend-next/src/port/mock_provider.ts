import type {
  ProviderCheckDraft,
  ModelEffort,
  ModelLimit,
  ModelRate,
  Protocol,
  ProviderCheck,
  ProviderRemoval,
  ProviderEdit,
  ProviderEntry,
  ProviderModelCheck,
  ProviderModelCheckRequest,
  ProviderPreset,
  ProviderPresetInstall,
  ProviderProbe,
} from "./port";
import { MockBoundary } from "./mock_boundary";

// The sources half of the fixture. Chained on for the reason the others are:
// MockPort satisfies AgentPort in one declaration, and each face keeps its own
// file.
export class MockProvider extends MockBoundary {
  // Whether this machine has been given a provider. The fixture opens without
  // one so the connect card can be designed, and every port the same window
  // builds reads the same answer: a key belongs to the machine, not to the
  // pane that asked for it. MockHub hands its ports one of these, so a second
  // window — or a second test — starts over.
  machine = { configured: false };


  // Three shapes the account grouping has to keep apart: one vendor reached
  // under two protocols, a custom relay serving other vendors' models, and two
  // tenants of that same relay holding different keys.
  private sources: ProviderEntry[] = [
    {
      name: "deepseek", kind: "openai", effortField: "reasoning_effort", baseUrl: "https://api.deepseek.com",
      models: ["deepseek-v4-pro", "deepseek-flash"], default: "deepseek-v4-pro",
      hasKey: true, inUse: true, preset: false, mark: "deepseek", keyEnv: "DEEPSEEK_API_KEY", canSetVision: false,
      balanceUrl: "https://api.deepseek.com/user/balance", impliedBalanceFormat: "deepseek", balanceFormats: ["deepseek", "moonshot", "openrouter"],
    },
    {
      name: "deepseek-anthropic", kind: "anthropic", baseUrl: "https://api.deepseek.com/anthropic",
      models: ["deepseek-v4-pro"], default: "deepseek-v4-pro",
      hasKey: true, inUse: false, preset: false, mark: "deepseek", keyEnv: "DEEPSEEK_API_KEY",
      canSetVision: false, canWebSearch: true, webSearch: true,
      canSetThinking: true, sendsThinking: true, effortField: "output_config.effort",
    },
    {
      name: "myrelay", kind: "openai", effortField: "reasoning_effort", baseUrl: "https://relay.example.com/v1",
      models: ["gpt-4o", "claude-sonnet-4"], default: "gpt-4o",
      hasKey: true, inUse: false, preset: false, mark: "openrouter", keyEnv: "MYRELAY_API_KEY",
      balanceUrl: "https://relay.example.com/user/balance", impliedBalanceFormat: "", balanceFormats: ["deepseek", "moonshot", "openrouter"],
      visionModels: ["gpt-4o"], canSetVision: true, canSetThinking: true, sendsThinking: true,
      reasoningProtocol: "openai", supportedEfforts: ["low", "medium", "high"],
      modelEfforts: { "claude-sonnet-4": { supportedEfforts: ["low", "high", "max"], defaultEffort: "high" } },
      contextWindow: 131072,
      modelLimits: { "claude-sonnet-4": { contextWindow: 200000, maxOutputTokens: 64000 } },
      modelPrices: { "gpt-4o": { input: 2.5, output: 10, cacheHit: 1.25, currency: "USD", sourceUrl: "https://relay.example.com/pricing", checked: "2026-10-17" } },
      inheritedLimits: { "gpt-4o": { contextWindow: 131072 }, "claude-sonnet-4": { contextWindow: 131072 } },
      inheritedEfforts: {
        "gpt-4o": { supportedEfforts: ["low", "medium", "high"] },
        "claude-sonnet-4": { supportedEfforts: ["low", "medium", "high"] },
      },
    },
    // The Responses wire is the one that can carry a turn by reference, so it
    // is the only entry here offering the choice.
    {
      name: "myrelay-responses", kind: "responses", baseUrl: "https://relay.example.com/v1",
      models: ["gpt-4o"], default: "gpt-4o",
      hasKey: true, inUse: false, preset: false, mark: "openrouter", keyEnv: "MYRELAY_API_KEY",
      canSetContinuation: true, continuation: "",
      canSetThinking: true, sendsThinking: true, effortField: "reasoning.effort",
    },
    {
      name: "myrelay-work", kind: "openai", effortField: "reasoning_effort", baseUrl: "https://relay.example.com/v1",
      models: ["gpt-4o"], default: "gpt-4o",
      hasKey: true, inUse: false, preset: false, keyEnv: "MYRELAY_WORK_API_KEY",
    },
  ];

  // Like the kernel, a plain re-check is remembered for the list; one made with
  // the form's own values is not.
  private readonly lastChecks = new Map<string, NonNullable<ProviderEntry["lastCheck"]>>();

  async providers(): Promise<ProviderEntry[]> {
    return this.sources.map((s) => (this.lastChecks.has(s.name) ? { ...s, lastCheck: this.lastChecks.get(s.name) } : s));
  }

  // Mirrors the kernel catalog: two wires answer one OpenAI model listing.
  async protocols(): Promise<Protocol[]> {
    return [
      { kind: "openai", discovery: "openai", serverWebSearch: false, reasoningParams: true, effortField: "reasoning_effort", effortUnder: ["openai"] },
      { kind: "responses", discovery: "openai", serverWebSearch: true, reasoningParams: true, effortField: "reasoning.effort", effortUnder: [] },
      { kind: "anthropic", discovery: "anthropic", serverWebSearch: true, reasoningParams: false, effortField: "output_config.effort", effortUnder: ["anthropic"] },
    ];
  }

  async providerPresets(): Promise<ProviderPreset[]> {
    const p = (id: string, label: string, vendor: string, kind = "openai", baseUrl = `https://api.${vendor}.example/v1`): ProviderPreset => ({ id, label, kind, baseUrl, vendor });
    return [
      p("deepseek-responses", "DeepSeek Official Responses API", "deepseek", "responses", "https://api.deepseek.com"),
      p("glm-cn", "GLM CN API", "zhipu", "openai", "https://open.bigmodel.cn/api/paas/v4"),
      p("zai-global", "Z.AI Global API", "zhipu", "openai", "https://api.z.ai/api/paas/v4"),
      p("kimi-cn", "Kimi CN API", "kimi", "openai", "https://api.moonshot.cn/v1"),
      p("kimi-global", "Kimi Global API", "kimi", "openai", "https://api.moonshot.ai/v1"),
      p("minimax-cn-api", "MiniMax CN API", "minimax", "openai", "https://api.minimaxi.com/v1"),
      p("qwen-cn", "Qwen CN API", "qwen", "openai", "https://dashscope.aliyuncs.com/compatible-mode/v1"),
      p("mimo-api", "MiMo API", "mimo", "openai", "https://api.xiaomimimo.com/v1"),
      p("longcat-openai", "LongCat OpenAI", "longcat", "openai", "https://api.longcat.chat/openai/v1"),
      p("stepfun", "StepFun", "stepfun", "openai", "https://api.stepfun.com/v1"),
      p("openrouter", "OpenRouter", "openrouter", "openai", "https://openrouter.ai/api/v1"),
      p("openai", "OpenAI", "openai", "openai", "https://api.openai.com/v1"),
      p("gemini", "Google Gemini", "google", "openai", "https://generativelanguage.googleapis.com/v1beta/openai"),
      p("token-rhythm", "Token Rhythm", "token-rhythm", "openai", "https://tokenrhythm.studio/v1"),
    ];
  }

  async installProviderPreset(req: ProviderPresetInstall): Promise<{ names: string[] }> {
    return { names: [req.name?.trim() || req.presetId] };
  }

  // The opening sequence is the only way into the fixture, and it advances on
  // what the probe found: refusing here leaves the demo on its first screen
  // with no way past, so it answers with data like every other face does.
  async probeProvider(): Promise<ProviderProbe> {
    return {
      kind: "openai", kinds: ["openai", "responses"], authHeader: true,
      models: ["deepseek-v4-pro", "deepseek-flash"], default: "deepseek-v4-pro",
      efforts: ["high", "max"], effort: "high", vision: [],
      ambiguous: true, noProxy: false,
    };
  }

  // The fixture's endpoints answer both protocols, which is the case the row's
  // "改用…" repair exists for. The relay answers at gateway scale, which is the
  // case the model list's search and its row cap exist for.
  async checkProvider(name: string, draft?: ProviderCheckDraft): Promise<ProviderCheck> {
    const got = await this.answerCheck(name);
    const plain = !draft?.baseUrl && !draft?.apiKey && !draft?.kind;
    if (plain) this.lastChecks.set(name, { at: Date.now(), ok: got.ok, ...(got.code ? { code: got.code } : {}) });
    return got;
  }

  protected async answerCheck(name: string): Promise<ProviderCheck> {
    if (name === "mimo") return { ok: false, code: "provider.probe.unauthorized", httpStatus: 401, detail: "Invalid Authentication" };
    const models = name.startsWith("myrelay")
      ? relayCatalog()
      : ["deepseek-v4-pro", "deepseek-flash", "deepseek-flash-vision-exp"];
    const vision = models.filter((m) => m === "gpt-4o" || m === "deepseek-flash-vision-exp");
    return { ok: true, kind: "openai", models, vision, ambiguous: true };
  }

  async checkProviderModel(request: ProviderModelCheckRequest): Promise<ProviderModelCheck> {
    await new Promise((resolve) => setTimeout(resolve, 280));
    if (request.model.includes("missing")) {
      return { model: request.model, status: "unavailable", reason: "not_found" };
    }
    if (request.model.includes("refused")) {
      return {
        model: request.model,
        status: "unknown",
        reason: "rejected",
        httpStatus: 400,
        detail: `registry.ollama.ai/library/${request.model} does not support tools`,
      };
    }
    return { model: request.model, status: "available" };
  }

  async saveProvider(): Promise<void> {
    this.machine.configured = true;
  }

  async renameProvider(names: string[], displayName: string): Promise<void> {
    const label = displayName.trim();
    this.sources = this.sources.map((p) => (names.includes(p.name) ? { ...p, displayName: label || undefined } : p));
  }

  // The kernel refuses a null anywhere in the extra body — TOML cannot hold one,
  // so it would be dropped on save and the field would never reach the wire.
  async editProvider(edit: ProviderEdit): Promise<void> {
    if (edit.extraBody && JSON.stringify(edit.extraBody).includes("null")) {
      throw new Error("extra body field cannot be null");
    }
    this.sources = this.sources.map((p) =>
      p.name === edit.name
        ? {
            ...p, models: edit.models, default: edit.default, visionModels: edit.vision,
            contextWindow: edit.contextWindow, maxOutputTokens: edit.maxOutputTokens,
            idleTimeoutSeconds: edit.idleTimeoutSeconds || undefined,
            balanceUrl: edit.balanceUrl ?? p.balanceUrl, balanceFormat: edit.balanceFormat ?? p.balanceFormat,
            headers: edit.headers, extraBody: edit.extraBody,
            reasoningProtocol: edit.reasoningProtocol,
            sendsThinking: edit.reasoningProtocol === undefined ? p.sendsThinking : edit.reasoningProtocol !== "none",
            webSearch: edit.webSearch ?? p.webSearch, continuation: edit.continuation ?? p.continuation,
            supportedEfforts: edit.supportedEfforts,
            defaultEffort: edit.defaultEffort,
            modelEfforts: edit.modelEfforts ? mockModelEfforts(edit.modelEfforts) : p.modelEfforts,
            modelLimits: edit.modelLimits ? mockModelLimits(edit.modelLimits) : p.modelLimits,
            modelPrices: edit.modelPrices ? mockModelPrices(edit.modelPrices, edit.models, p.modelPrices) : p.modelPrices,
          }
        : p,
    );
  }

  // The relay stands for a service other roles lean on, so removing it shows
  // the report; the rest go without a word.
  async removeProvider(name: string): Promise<ProviderRemoval> {
    this.sources = this.sources.filter((e) => e.name !== name);
    return name === "myrelay"
      ? { movedTo: "deepseek", moved: ["default", "planner"], cleared: ["vision", "advisor"] }
      : { movedTo: "", moved: [], cleared: [] };
  }
}

// A gateway answering at the scale the connection panel has to survive: the
// families repeat under date suffixes, which is exactly what makes such a list
// unscannable by eye. Issue #9192 is one of these, with 216 entries.
function relayCatalog(): string[] {
  const families = [
    "qwen3-max", "qwen3-omni-flash", "qwen3-vl-plus", "qwen3-next-80b-a3b",
    "MiniMax-M2", "MiniMax/speech-02", "ZHIPU/GLM-5", "kimi-k2",
    "deepseek-v4-pro", "deepseek-flash", "gpt-4o", "claude-sonnet-4",
    "gemini-3-pro", "llama-4-maverick", "mistral-large", "grok-4",
  ];
  const out: string[] = [];
  for (const f of families) {
    out.push(f);
    for (const s of ["preview", "realtime", "2025-09-15", "2025-12-01", "2026-01-23", "2026-02-23"]) {
      out.push(`${f}-${s}`);
    }
  }
  return out;
}

// The kernel keeps only models that declare levels; the rest inherit.
function mockModelEfforts(sent: Record<string, ModelEffort>): Record<string, ModelEffort> | undefined {
  const kept = Object.entries(sent).filter(([, v]) => v.supportedEfforts.length > 0);
  return kept.length ? Object.fromEntries(kept) : undefined;
}

function mockModelLimits(sent: Record<string, ModelLimit>): Record<string, ModelLimit> | undefined {
  const kept = Object.entries(sent).filter(([, v]) => v.contextWindow || v.maxOutputTokens);
  return kept.length ? Object.fromEntries(kept) : undefined;
}

// A listed model sent null has none of its own; one left out of the answer keeps
// what it had, as the kernel does.
function mockModelPrices(sent: Record<string, ModelRate | null>, models: string[], was: Record<string, ModelRate> | undefined): Record<string, ModelRate> | undefined {
  const next: Record<string, ModelRate> = { ...(was ?? {}) };
  for (const model of models) {
    const rate = sent[model];
    if (rate) next[model] = rate;
    else delete next[model];
  }
  return Object.keys(next).length ? next : undefined;
}
