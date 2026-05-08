/** apiKey is write-only on the wire; GET always returns a redacted form so dashboard screenshots don't leak credentials. */

import { isPlausibleKey, readConfig, redactKey, saveEditMode, writeConfig } from "../../config.js";
import { getLanguage, getSupportedLanguages, setLanguage } from "../../i18n/index.js";
import type { LanguageCode } from "../../i18n/types.js";
import type { DashboardContext } from "../context.js";
import type { ApiResult } from "../router.js";

interface SettingsBody {
  apiKey?: unknown;
  baseUrl?: unknown;
  lang?: unknown;
  preset?: unknown;
  reasoningEffort?: unknown;
  search?: unknown;
  model?: unknown;
  proNext?: unknown;
  budgetUsd?: unknown;
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

// Accept new (auto/flash/pro) and legacy (fast/smart/max) — server
// stores whatever the user picked; resolvePreset() canonicalizes at
// read time. Web sends new names in 0.12.x onward.
const VALID_PRESETS = new Set(["auto", "flash", "pro", "fast", "smart", "max"]);
const VALID_EFFORTS = new Set(["high", "max"]);

export async function handleSettings(
  method: string,
  _rest: string[],
  body: string,
  ctx: DashboardContext,
): Promise<ApiResult> {
  if (method === "GET") {
    const cfg = readConfig(ctx.configPath);
    const live = ctx.loop;
    return {
      status: 200,
      body: {
        apiKey: cfg.apiKey ? redactKey(cfg.apiKey) : null,
        apiKeySet: Boolean(cfg.apiKey),
        baseUrl: cfg.baseUrl ?? null,
        lang: getLanguage(),
        preset: cfg.preset ?? "auto",
        reasoningEffort: cfg.reasoningEffort ?? "max",
        search: cfg.search !== false,
        editMode: cfg.editMode ?? "review",
        session: cfg.session ?? null,
        model: live?.model ?? null,
        proNext: live?.proArmed ?? false,
        budgetUsd: live?.budgetUsd ?? null,
        // Hint to the SPA which fields require restart.
        appliesAt: {
          apiKey: "next-session",
          baseUrl: "next-session",
          preset: "next-session",
          reasoningEffort: "next-turn",
          search: "next-session",
          model: "next-turn",
          proNext: "next-turn",
          budgetUsd: "live",
        },
      },
    };
  }

  if (method === "POST") {
    const fields = parseBody(body);
    // Single read up top, all field updates accumulate, single writeConfig at the end —
    // a per-field write would clobber earlier per-field writes from the same POST.
    const cfg = readConfig(ctx.configPath);
    const changed: string[] = [];
    let langPending: LanguageCode | null = null;
    let presetPendingLive: string | null = null;
    let effortPendingLive: "high" | "max" | null = null;

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
        return { status: 400, body: { error: "apiKey must be a plausible sk- token" } };
      }
      cfg.apiKey = fields.apiKey.trim();
      changed.push("apiKey");
    }
    if (fields.baseUrl !== undefined) {
      if (typeof fields.baseUrl !== "string" || !fields.baseUrl.trim()) {
        return { status: 400, body: { error: "baseUrl must be a non-empty string" } };
      }
      cfg.baseUrl = fields.baseUrl.trim();
      changed.push("baseUrl");
    }
    if (fields.preset !== undefined) {
      if (typeof fields.preset !== "string" || !VALID_PRESETS.has(fields.preset)) {
        return { status: 400, body: { error: "preset must be auto | flash | pro" } };
      }
      cfg.preset = fields.preset as "auto" | "flash" | "pro" | "fast" | "smart" | "max";
      presetPendingLive = fields.preset;
      changed.push("preset");
    }
    if (fields.reasoningEffort !== undefined) {
      if (
        typeof fields.reasoningEffort !== "string" ||
        !VALID_EFFORTS.has(fields.reasoningEffort)
      ) {
        return { status: 400, body: { error: "reasoningEffort must be high | max" } };
      }
      cfg.reasoningEffort = fields.reasoningEffort as "high" | "max";
      effortPendingLive = fields.reasoningEffort as "high" | "max";
      changed.push("reasoningEffort");
    }
    if (fields.search !== undefined) {
      if (typeof fields.search !== "boolean") {
        return { status: 400, body: { error: "search must be a boolean" } };
      }
      cfg.search = fields.search;
      changed.push("search");
    }
    let modelPendingLive: string | null = null;
    let proNextPending: boolean | null = null;
    let budgetPending: number | null | undefined;
    if (fields.model !== undefined) {
      if (typeof fields.model !== "string" || !fields.model.trim()) {
        return { status: 400, body: { error: "model must be a non-empty string" } };
      }
      // Model is live-only (not in ReasonixConfig). Same as /model <id> slash — disk
      // pickup goes through preset / startup flag, not direct cfg.model.
      modelPendingLive = fields.model.trim();
      changed.push("model");
    }
    if (fields.proNext !== undefined) {
      if (typeof fields.proNext !== "boolean") {
        return { status: 400, body: { error: "proNext must be a boolean" } };
      }
      // Not persisted: arming is per-turn ephemeral. Live-only side effect.
      proNextPending = fields.proNext;
      changed.push("proNext");
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

    if (changed.length > 0) {
      writeConfig(cfg, ctx.configPath);
      // Runtime side-effects fire after the disk write succeeds —
      // prevents an i18n change from being visible while the on-disk
      // value still reflects the old setting (and vice-versa for
      // preset / reasoningEffort).
      if (langPending) setLanguage(langPending);
      if (presetPendingLive) ctx.applyPresetLive?.(presetPendingLive);
      if (effortPendingLive) ctx.applyEffortLive?.(effortPendingLive);
      if (modelPendingLive) ctx.applyModelLive?.(modelPendingLive);
      if (proNextPending !== null) ctx.setProNextLive?.(proNextPending);
      if (budgetPending !== undefined) ctx.setBudgetUsdLive?.(budgetPending);
      ctx.audit?.({ ts: Date.now(), action: "set-settings", payload: { fields: changed } });
    }
    return { status: 200, body: { changed } };
  }

  return { status: 405, body: { error: "GET or POST only" } };
}

// Keep saveEditMode imported so future GET responses can include the
// canonical default — used by the SPA when /api/overview hasn't yet
// resolved. (Currently surfaced via /api/overview directly.)
void saveEditMode;
