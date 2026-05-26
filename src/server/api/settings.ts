/** apiKey is write-only on the wire; GET always returns a redacted form so dashboard screenshots don't leak credentials. */

import {
  type EditMode,
  REASONING_EFFORT_VALUES,
  type ReasoningEffort,
  isPlausibleKey,
  isReasoningEffort,
  loadModel,
  normalizeSkillPathEntries,
  normalizeSkillPaths,
  readConfig,
  webSearchEngine as readWebSearchEngine,
  redactKey,
  saveEditMode,
  writeConfig,
} from "../../config.js";
import { getLanguage, getSupportedLanguages, setLanguage } from "../../i18n/index.js";
import type { LanguageCode } from "../../i18n/types.js";
import type { DashboardContext } from "../context.js";
import type { ApiResult } from "../router.js";

interface SettingsBody {
  apiKey?: unknown;
  baseUrl?: unknown;
  lang?: unknown;
  editMode?: unknown;
  reasoningEffort?: unknown;
  search?: unknown;
  webSearchEngine?: unknown;
  model?: unknown;
  budgetUsd?: unknown;
  skillPaths?: unknown;
  subagentModels?: unknown;
}

function parseBody(raw: string): SettingsBody {
  if (!raw) return {};
  try {
    const parsed = JSON.parse(raw);
    return typeof parsed === "object" && parsed !== null ? (parsed as SettingsBody) : {};
  } catch {
    return {};
  }
}

const VALID_WEB_SEARCH_ENGINES = new Set([
  "bing",
  "searxng",
  "metaso",
  "tavily",
  "perplexity",
  "exa",
  "ollama",
]);

const VALID_EDIT_MODES = new Set(["review", "auto", "yolo", "plan"]);

void saveEditMode;

export async function handleSettings(
  method: string,
  _rest: string[],
  body: string,
  ctx: DashboardContext,
): Promise<ApiResult> {
  if (method === "GET") {
    const cfg = readConfig(ctx.configPath);
    if (cfg.search === undefined) {
      cfg.search = true;
      writeConfig(cfg, ctx.configPath);
    }
    const live = ctx.loop;
    return {
      status: 200,
      body: {
        apiKey: cfg.apiKey ? redactKey(cfg.apiKey) : null,
        apiKeySet: Boolean(cfg.apiKey),
        baseUrl: cfg.baseUrl ?? null,
        lang: getLanguage(),
        reasoningEffort: isReasoningEffort(cfg.reasoningEffort) ? cfg.reasoningEffort : "high",
        search: cfg.search !== false,
        webSearchEngine: readWebSearchEngine(ctx.configPath),
        editMode: ctx.getEditMode?.() ?? cfg.editMode ?? "review",
        session: cfg.session ?? null,
        model: live?.model ?? loadModel(ctx.configPath),
        budgetUsd: live?.budgetUsd ?? null,
        sessionSpendUsd: ctx.getStats?.()?.totalCostUsd ?? null,
        skillPaths: normalizeSkillPaths(
          cfg.skills?.paths ?? [],
          ctx.getCurrentCwd?.() ?? process.cwd(),
        ),
        skillPathEntries: normalizeSkillPathEntries(
          cfg.skills?.paths ?? [],
          ctx.getCurrentCwd?.() ?? process.cwd(),
        ),
        subagentModels: cfg.subagentModels ?? {},
        appliesAt: {
          apiKey: "next-session",
          baseUrl: "next-session",
          reasoningEffort: "next-turn",
          search: "next-session",
          webSearchEngine: "next-turn",
          model: "next-turn",
          budgetUsd: "live",
          skillPaths: "next-session",
          subagentModels: "next-skill-run",
        },
      },
    };
  }

  if (method === "POST") {
    const fields = parseBody(body);
    const cfg = readConfig(ctx.configPath);
    const changed: string[] = [];
    let langPending: LanguageCode | null = null;
    let effortPendingLive: ReasoningEffort | null = null;

    if (fields.lang !== undefined) {
      const raw = String(fields.lang);
      const supported = getSupportedLanguages();
      const langCode = supported.find((l) => l.toLowerCase() === raw.toLowerCase()) as
        | LanguageCode
        | undefined;
      if (!langCode) {
        return { status: 400, body: { error: `lang must be one of: ${supported.join(", ")}` } };
      }
      cfg.lang = langCode;
      langPending = langCode;
      changed.push("lang");
    }
    if (fields.apiKey !== undefined) {
      if (typeof fields.apiKey !== "string" || !isPlausibleKey(fields.apiKey)) {
        return { status: 400, body: { error: "apiKey must be 16+ chars with no whitespace" } };
      }
      cfg.apiKey = fields.apiKey.trim();
      changed.push("apiKey");
    }
    if (fields.baseUrl !== undefined) {
      if (typeof fields.baseUrl !== "string") {
        return { status: 400, body: { error: "baseUrl must be a string" } };
      }
      const trimmed = fields.baseUrl.trim();
      cfg.baseUrl = trimmed.length > 0 ? trimmed : undefined;
      changed.push("baseUrl");
    }
    if (fields.editMode !== undefined) {
      if (typeof fields.editMode !== "string" || !VALID_EDIT_MODES.has(fields.editMode)) {
        return { status: 400, body: { error: "editMode must be review | auto | yolo | plan" } };
      }
      cfg.editMode = fields.editMode as EditMode;
      changed.push("editMode");
    }
    if (fields.reasoningEffort !== undefined) {
      const raw =
        typeof fields.reasoningEffort === "string" ? fields.reasoningEffort.toLowerCase() : "";
      if (!isReasoningEffort(raw)) {
        return {
          status: 400,
          body: { error: `reasoningEffort must be one of: ${REASONING_EFFORT_VALUES.join(" | ")}` },
        };
      }
      cfg.reasoningEffort = raw;
      effortPendingLive = raw;
      changed.push("reasoningEffort");
    }
    if (fields.search !== undefined) {
      if (typeof fields.search !== "boolean") {
        return { status: 400, body: { error: "search must be a boolean" } };
      }
      cfg.search = fields.search;
      changed.push("search");
    }
    if (fields.webSearchEngine !== undefined) {
      if (
        typeof fields.webSearchEngine !== "string" ||
        !VALID_WEB_SEARCH_ENGINES.has(fields.webSearchEngine)
      ) {
        return {
          status: 400,
          body: {
            error:
              "webSearchEngine must be bing | searxng | metaso | tavily | perplexity | exa | ollama",
          },
        };
      }
      cfg.webSearchEngine = fields.webSearchEngine as
        | "bing"
        | "searxng"
        | "metaso"
        | "tavily"
        | "perplexity"
        | "exa"
        | "ollama";
      changed.push("webSearchEngine");
    }
    let modelPendingLive: string | null = null;
    let budgetPending: number | null | undefined;
    if (fields.model !== undefined) {
      if (typeof fields.model !== "string" || !fields.model.trim()) {
        return { status: 400, body: { error: "model must be a non-empty string" } };
      }
      const trimmed = fields.model.trim();
      cfg.model = trimmed;
      modelPendingLive = trimmed;
      changed.push("model");
    }
    if (fields.budgetUsd !== undefined) {
      if (fields.budgetUsd === null) {
        budgetPending = null;
      } else if (
        typeof fields.budgetUsd === "number" &&
        fields.budgetUsd > 0 &&
        Number.isFinite(fields.budgetUsd)
      ) {
        budgetPending = fields.budgetUsd;
      } else {
        return {
          status: 400,
          body: { error: "budgetUsd must be null or a positive finite number" },
        };
      }
      changed.push("budgetUsd");
    }

    if (fields.skillPaths !== undefined) {
      const raw = Array.isArray(fields.skillPaths)
        ? fields.skillPaths
        : typeof fields.skillPaths === "string"
          ? fields.skillPaths.split(",")
          : null;
      if (!raw) {
        return { status: 400, body: { error: "skillPaths must be a string or string[]" } };
      }
      cfg.skills = {
        ...(cfg.skills ?? {}),
        paths: normalizeSkillPaths(raw, ctx.getCurrentCwd?.() ?? process.cwd()),
      };
      changed.push("skillPaths");
    }

    if (fields.subagentModels !== undefined) {
      if (
        typeof fields.subagentModels !== "object" ||
        fields.subagentModels === null ||
        Array.isArray(fields.subagentModels)
      ) {
        return {
          status: 400,
          body: { error: "subagentModels must be an object mapping skill name → 'flash' | 'pro'" },
        };
      }
      const sanitized = new Map<string, "flash" | "pro">();
      for (const [name, value] of Object.entries(fields.subagentModels)) {
        if (typeof name !== "string" || !name) continue;
        if (name === "__proto__" || name === "constructor" || name === "prototype") continue;
        if (value === "flash" || value === "pro") sanitized.set(name, value);
      }
      cfg.subagentModels = sanitized.size > 0 ? Object.fromEntries(sanitized) : undefined;
      changed.push("subagentModels");
    }

    if (changed.length > 0) {
      writeConfig(cfg, ctx.configPath);
      if (langPending) setLanguage(langPending);
      if (fields.editMode !== undefined) {
        const mode = fields.editMode as EditMode;
        if (ctx.setEditMode) ctx.setEditMode(mode);
        else saveEditMode(mode, ctx.configPath);
      }
      if (effortPendingLive) ctx.applyEffortLive?.(effortPendingLive);
      if (modelPendingLive) ctx.applyModelLive?.(modelPendingLive);
      if (budgetPending !== undefined) ctx.setBudgetUsdLive?.(budgetPending);
      ctx.audit?.({ ts: Date.now(), action: "set-settings", payload: { fields: changed } });
    }
    return { status: 200, body: { changed } };
  }

  return { status: 405, body: { error: "GET or POST only" } };
}
