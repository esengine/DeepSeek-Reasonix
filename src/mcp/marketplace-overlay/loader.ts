import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

export interface OverlayEntry {
  title: string;
  description: string;
}

let cache: Record<string, OverlayEntry> | null = null;
let cachedLang: string | null = null;

export function loadOverlay(lang: string): Record<string, OverlayEntry> | null {
  if (cachedLang === lang && cache) return cache;
  try {
    const dir = dirname(fileURLToPath(import.meta.url));
    const raw = readFileSync(join(dir, `${lang}.json`), "utf8");
    cache = JSON.parse(raw) as Record<string, OverlayEntry>;
    cachedLang = lang;
    return cache;
  } catch {
    return null;
  }
}
