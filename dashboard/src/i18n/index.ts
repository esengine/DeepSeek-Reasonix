import { useEffect, useState } from "react";
import { de } from "./de";
import { en } from "./en";
import { zhCN } from "./zh-CN";

export type Lang = "en" | "zh-CN" | "de";

const SUPPORTED_LANGS: readonly Lang[] = ["en", "zh-CN", "de"];
const SUPPORTED: ReadonlySet<Lang> = new Set<Lang>(SUPPORTED_LANGS);
const LANG_LABELS: Record<Lang, string> = { en: "English", "zh-CN": "简体中文", de: "Deutsch" };
const STORAGE_KEY = "reasonix.lang";

type Listener = () => void;
const listeners: Listener[] = [];

function detectDefault(): Lang {
  try {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (stored && SUPPORTED.has(stored as Lang)) return stored as Lang;
  } catch {
    /* private mode */
  }
  const nav = typeof navigator !== "undefined" ? navigator.language : "";
  const lower = nav.toLowerCase();
  if (lower.startsWith("zh")) return "zh-CN";
  if (lower.startsWith("de")) return "de";
  return "en";
}

let currentLang: Lang = detectDefault();

function syncHtmlLang(lang: Lang): void {
  if (typeof document === "undefined") return;
  try {
    document.documentElement.lang = lang;
  } catch {
    /* document not ready */
  }
}

syncHtmlLang(currentLang);

export function getLang(): Lang {
  return currentLang;
}

export function getSupportedLangs(): readonly Lang[] {
  return SUPPORTED_LANGS;
}

export function getLangLabel(lang: Lang): string {
  return LANG_LABELS[lang];
}

export function setLang(lang: Lang): void {
  if (!SUPPORTED.has(lang) || lang === currentLang) return;
  currentLang = lang;
  try {
    localStorage.setItem(STORAGE_KEY, lang);
  } catch {
    /* ignore */
  }
  syncHtmlLang(lang);
  for (const cb of listeners) cb();
}

export function useLang(): Lang {
  const [, setTick] = useState(0);
  useEffect(() => {
    const cb = () => setTick((n) => n + 1);
    listeners.push(cb);
    return () => {
      const idx = listeners.indexOf(cb);
      if (idx >= 0) listeners.splice(idx, 1);
    };
  }, []);
  return currentLang;
}

const dicts = { en, "zh-CN": zhCN, de } as const;

type Dict = typeof en;
type Path<T, K extends keyof T = keyof T> = K extends string
  ? T[K] extends Record<string, unknown>
    ? `${K}.${Path<T[K]>}`
    : K
  : never;
export type TKey = Path<Dict>;

function resolve(dict: Dict, key: string): string {
  const parts = key.split(".");
  let cursor: unknown = dict;
  for (const p of parts) {
    if (cursor && typeof cursor === "object" && p in (cursor as Record<string, unknown>)) {
      cursor = (cursor as Record<string, unknown>)[p];
    } else {
      return key;
    }
  }
  return typeof cursor === "string" ? cursor : key;
}

export function t(key: TKey, params?: Record<string, string | number>): string {
  const raw = resolve(dicts[currentLang], key);
  if (!params) return raw;
  return raw.replace(/\{(\w+)\}/g, (_, k) => (k in params ? String(params[k]) : `{${k}}`));
}
