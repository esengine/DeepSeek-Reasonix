import { useEffect, useState } from "preact/hooks";
import { TOKEN, api } from "./api.js";

type Listener = () => void;

export type DashboardLang = "en" | "zh-CN";

// [dashboardCode, backendCode] — add new languages here.
const LANG_REGISTRY: [DashboardLang, string][] = [
  ["en", "EN"],
  ["zh-CN", "zh-CN"],
];

const SUPPORTED = new Set(LANG_REGISTRY.map(([d]) => d));
const TO_BACKEND = new Map(LANG_REGISTRY);
const FROM_BACKEND = new Map(LANG_REGISTRY.map(([d, b]) => [b, d]));

const STORAGE_KEY = "rx.lang";
const listeners: Listener[] = [];
let currentLang: DashboardLang = loadFromStorage() ?? "en";

function loadFromStorage(): DashboardLang | null {
  try {
    const v = localStorage.getItem(STORAGE_KEY);
    if (v !== null && SUPPORTED.has(v as DashboardLang)) return v as DashboardLang;
  } catch {
    /* private mode */
  }
  return null;
}

function toBackendLang(lang: DashboardLang): string {
  return TO_BACKEND.get(lang) ?? lang;
}

function fromBackendLang(raw: string): DashboardLang {
  return (FROM_BACKEND.get(raw) as DashboardLang) ?? "en";
}

/** Adopt server lang on startup; localStorage is render-cache only, never pushed back. */
export async function initLangFromServer(): Promise<void> {
  try {
    const res = await api<{ lang?: string }>("/settings");
    const serverLang = res.lang ? fromBackendLang(res.lang) : null;
    if (!serverLang || serverLang === currentLang) return;
    currentLang = serverLang;
    try {
      localStorage.setItem(STORAGE_KEY, serverLang);
    } catch {
      /* ignore */
    }
    for (const cb of listeners) cb();
  } catch {
    /* offline — keep last-known value rendering */
  }
}

export function getLang(): DashboardLang {
  return currentLang;
}

export function setLang(lang: DashboardLang): void {
  if (!SUPPORTED.has(lang)) return;
  currentLang = lang;
  try {
    localStorage.setItem(STORAGE_KEY, lang);
  } catch {
    /* ignore */
  }
  for (const cb of listeners) cb();
  // keepalive ensures the request completes even during page unload (refresh).
  fetch(`/api/settings?token=${TOKEN}`, {
    method: "POST",
    headers: { "Content-Type": "application/json", "X-Reasonix-Token": TOKEN },
    body: JSON.stringify({ lang: toBackendLang(lang) }),
    keepalive: true,
  }).catch((err) => console.error("[reasonix dashboard] lang persist:", err));
}

export function onLangChange(cb: Listener): () => void {
  listeners.push(cb);
  return () => {
    const i = listeners.indexOf(cb);
    if (i >= 0) listeners.splice(i, 1);
  };
}

export function useLang(): DashboardLang {
  const [lang, setLangState] = useState<DashboardLang>(currentLang);
  useEffect(() => onLangChange(() => setLangState(currentLang)), []);
  return lang;
}

type Nested = { [k: string]: string | Nested };

function get(translations: Nested | undefined, path: string): string | undefined {
  let val: string | Nested | undefined = translations;
  for (const part of path.split(".")) {
    if (val === undefined || typeof val === "string") return undefined;
    val = val[part];
  }
  return typeof val === "string" ? val : undefined;
}

export function createT(translations: Record<string, Nested>) {
  return function t(path: string, params?: Record<string, string | number>): string {
    let val = get(translations[currentLang] ?? translations.en, path);
    if (val === undefined) val = get(translations.en, path);
    if (val === undefined) return path;
    if (!params) return val;
    let result = val;
    for (const [k, v] of Object.entries(params)) {
      result = result.replaceAll(`{${k}}`, String(v));
    }
    return result;
  };
}
