/** Localization overlay for marketplace entries — swaps title/description for known
 * registry names. Bundled JSON is validated at module load so malformed data fails fast. */

import { readFileSync } from "node:fs";
import type { LanguageCode } from "../../i18n/types.js";
import type { RegistryEntry } from "../registry-types.js";
import zhCNRaw from "./zh-CN.json";

export interface OverlayEntry {
  title: string;
  description: string;
}

export type Overlay = Record<string, OverlayEntry>;

function validate(raw: unknown, label: string): Overlay {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    throw new Error(`marketplace overlay ${label} must be a JSON object`);
  }
  const overlay: Overlay = {};
  for (const [key, value] of Object.entries(raw as Record<string, unknown>)) {
    if (!value || typeof value !== "object" || Array.isArray(value)) {
      throw new Error(`marketplace overlay ${label}: entry "${key}" must be an object`);
    }
    const v = value as { title?: unknown; description?: unknown };
    if (typeof v.title !== "string" || typeof v.description !== "string") {
      throw new Error(
        `marketplace overlay ${label}: entry "${key}" needs string title + description`,
      );
    }
    overlay[key] = { title: v.title, description: v.description };
  }
  return overlay;
}

const BUNDLED: Partial<Record<LanguageCode, Overlay>> = {
  "zh-CN": validate(zhCNRaw, "zh-CN.json"),
};

const cache = new Map<LanguageCode, Overlay>();

/** Returns an empty overlay for languages without a bundled file. Throws on malformed JSON.
 * `overlayPath` is a test-only escape hatch that bypasses the bundled cache and reads
 * from disk, so failure modes (malformed JSON, bad shape) can be exercised. */
export function loadOverlay(lang: LanguageCode, overlayPath?: string): Overlay {
  if (overlayPath) {
    let raw: string;
    try {
      raw = readFileSync(overlayPath, "utf8");
    } catch {
      return {};
    }
    let parsed: unknown;
    try {
      parsed = JSON.parse(raw);
    } catch (err) {
      throw new Error(`marketplace overlay ${lang}.json is malformed: ${(err as Error).message}`);
    }
    return validate(parsed, `${lang}.json`);
  }
  const cached = cache.get(lang);
  if (cached) return cached;
  const overlay = BUNDLED[lang] ?? {};
  cache.set(lang, overlay);
  return overlay;
}

export interface ApplyOverlayResult {
  /** Localized title when an overlay entry exists, otherwise the upstream title. */
  title: string;
  /** Localized description when an overlay entry exists, otherwise the upstream description. */
  description: string;
  /** Upstream English title — set only when an overlay hit replaced the title, so callers
   * can render it as a dimmed secondary line. */
  englishTitle?: string;
}

/** Returns the entry's localized title/description, falling back to upstream verbatim. */
export function applyOverlay(entry: RegistryEntry, lang: LanguageCode): ApplyOverlayResult {
  const overlay = loadOverlay(lang);
  const hit = overlay[entry.name];
  if (!hit) {
    return { title: entry.title, description: entry.description };
  }
  return { title: hit.title, description: hit.description, englishTitle: entry.title };
}

/** Test-only — drops cached overlays so a follow-up loadOverlay re-reads bundled data. */
export function clearOverlayCache(): void {
  cache.clear();
}
