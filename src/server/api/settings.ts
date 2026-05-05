/** apiKey is write-only on the wire; GET always returns a redacted form so dashboard screenshots don't leak credentials. */

import { isPlausibleKey, readConfig, redactKey, saveEditMode, writeConfig } from "../../config.js";
import { getLanguage, getSupportedLanguages, setLanguageRuntime } from "../../i18n/index.js";
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
        model: ctx.loop?.model ?? null,
        // Hint to the SPA which fields require restart.
        appliesAt: {
          apiKey: "next-session",
          baseUrl: "next-session",
          preset: "next-session",
          reasoningEffort: "next-turn",
          search: "next-session",
        },
      },
    };
  }

  if (method === "POST") {
    const fields = parseBody(body);
    const cfg = readConfig(ctx.configPath);
    const changed: string[] = [];
    let cfgDirty = false;

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
      setLanguageRuntime(langCode);
      cfgDirty = true;
      changed.push("lang");
    }

    if (fields.apiKey !== undefined) {
      if (typeof fields.apiKey !== "string" || !isPlausibleKey(fields.apiKey)) {
        return { status: 400, body: { error: "apiKey must be a plausible sk- token" } };
      }
      cfg.apiKey = fields.apiKey.trim();
      cfgDirty = true;
      changed.push("apiKey");
    }

    if (fields.baseUrl !== undefined) {
      if (typeof fields.baseUrl !== "string" || !fields.baseUrl.trim()) {
        return { status: 400, body: { error: "baseUrl must be a non-empty string" } };
      }
      cfg.baseUrl = fields.baseUrl.trim();
      cfgDirty = true;
      changed.push("baseUrl");
    }

    if (fields.preset !== undefined) {
      if (typeof fields.preset !== "string" || !VALID_PRESETS.has(fields.preset)) {
        return { status: 400, body: { error: "preset must be auto | flash | pro" } };
      }
      cfg.preset = fields.preset as "auto" | "flash" | "pro" | "fast" | "smart" | "max";
      cfgDirty = true;
      changed.push("preset");
      // Apply to the LIVE loop too so the user doesn't have to restart
      // their session to feel the change. The callback canonicalizes
      // legacy aliases via resolvePreset internally.
      ctx.applyPresetLive?.(fields.preset);
    }

    if (fields.reasoningEffort !== undefined) {
      if (
        typeof fields.reasoningEffort !== "string" ||
        !VALID_EFFORTS.has(fields.reasoningEffort)
      ) {
        return { status: 400, body: { error: "reasoningEffort must be high | max" } };
      }
      cfg.reasoningEffort = fields.reasoningEffort as "high" | "max";
      cfgDirty = true;
      changed.push("reasoningEffort");
      ctx.applyEffortLive?.(fields.reasoningEffort as "high" | "max");
    }

    if (fields.search !== undefined) {
      if (typeof fields.search !== "boolean") {
        return { status: 400, body: { error: "search must be a boolean" } };
      }
      cfg.search = fields.search;
      cfgDirty = true;
      changed.push("search");
    }

    if (cfgDirty) {
      writeConfig(cfg, ctx.configPath);
    }

    if (changed.length > 0) {
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
