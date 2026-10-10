import type {
  ModelEffort,
  ModelLimit,
  Protocol,
  ProviderCheck,
  ProviderRemoval,
  ProviderEdit,
  ProviderEntry,
  ProviderModelCheck,
  ProviderModelCheckRequest,
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
      hasKey: true, inUse: true, preset: false, keyEnv: "DEEPSEEK_API_KEY", canSetVision: false,
    },
    {
      name: "deepseek-anthropic", kind: "anthropic", baseUrl: "https://api.deepseek.com/anthropic",
      models: ["deepseek-v4-pro"], default: "deepseek-v4-pro",
      hasKey: true, inUse: false, preset: false, keyEnv: "DEEPSEEK_API_KEY",
      canSetVision: false, canWebSearch: true, webSearch: true,
      canSetThinking: true, sendsThinking: true, effortField: "output_config.effort",
    },
    {
      name: "myrelay", kind: "openai", effortField: "reasoning_effort", baseUrl: "https://relay.example.com/v1",
      models: ["gpt-4o", "claude-sonnet-4"], default: "gpt-4o",
      hasKey: true, inUse: false, preset: false, keyEnv: "MYRELAY_API_KEY",
      visionModels: ["gpt-4o"], canSetVision: true, canSetThinking: true, sendsThinking: true,
      reasoningProtocol: "openai", supportedEfforts: ["low", "medium", "high"],
      modelEfforts: { "claude-sonnet-4": { supportedEfforts: ["low", "high", "max"], defaultEffort: "high" } },
      contextWindow: 131072,
      modelLimits: { "claude-sonnet-4": { contextWindow: 200000, maxOutputTokens: 64000 } },
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
      hasKey: true, inUse: false, preset: false, keyEnv: "MYRELAY_API_KEY",
      canSetContinuation: true, continuation: "",
      canSetThinking: true, sendsThinking: true, effortField: "reasoning.effort",
    },
    {
      name: "myrelay-work", kind: "openai", effortField: "reasoning_effort", baseUrl: "https://relay.example.com/v1",
      models: ["gpt-4o"], default: "gpt-4o",
      hasKey: true, inUse: false, preset: false, keyEnv: "MYRELAY_WORK_API_KEY",
    },
  ];

  async providers(): Promise<ProviderEntry[]> {
    return this.sources;
  }

  // Mirrors the kernel catalog: two wires answer one OpenAI model listing.
  async protocols(): Promise<Protocol[]> {
    return [
      { kind: "openai", discovery: "openai", serverWebSearch: false, reasoningParams: true, effortField: "reasoning_effort", effortUnder: ["openai"] },
      { kind: "responses", discovery: "openai", serverWebSearch: true, reasoningParams: true, effortField: "reasoning.effort", effortUnder: [] },
      { kind: "anthropic", discovery: "anthropic", serverWebSearch: true, reasoningParams: false, effortField: "output_config.effort", effortUnder: ["anthropic"] },
    ];
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
  async checkProvider(name: string): Promise<ProviderCheck> {
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

  async setProviderWebSearch(name: string, on: boolean): Promise<void> {
    this.sources = this.sources.map((p) => (p.name === name ? { ...p, webSearch: on } : p));
  }

  async setProviderContinuation(name: string, mode: string): Promise<void> {
    this.sources = this.sources.map((p) => (p.name === name ? { ...p, continuation: mode } : p));
  }

  async renameProvider(names: string[], displayName: string): Promise<void> {
    const label = displayName.trim();
    this.sources = this.sources.map((p) => (names.includes(p.name) ? { ...p, displayName: label || undefined } : p));
  }

  async setProviderThinking(name: string, on: boolean): Promise<void> {
    this.sources = this.sources.map((p) => (p.name === name ? { ...p, sendsThinking: on } : p));
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
            headers: edit.headers, extraBody: edit.extraBody,
            reasoningProtocol: edit.reasoningProtocol, supportedEfforts: edit.supportedEfforts,
            defaultEffort: edit.defaultEffort,
            modelEfforts: edit.modelEfforts ? mockModelEfforts(edit.modelEfforts) : p.modelEfforts,
            modelLimits: edit.modelLimits ? mockModelLimits(edit.modelLimits) : p.modelLimits,
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
