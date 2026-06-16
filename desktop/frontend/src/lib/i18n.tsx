// i18n is the desktop's localization seam. It mirrors theme.ts's "persist a choice
// and apply it" shape, but UI text must re-render on a switch, so the active locale
// lives in React state behind a context — flipping it re-renders the whole tree
// (App is a child of the provider). A module-level mirror (`currentLocale`) lets
// non-React code (lib/tools.ts) translate too; it stays fresh because the provider
// updates it on every render.
//
// Desktop UI language is intentionally separate from the CLI/kernel `language`
// config for prompts and terminal text. The desktop preference is persisted in
// the user-level [desktop] config; localStorage is only read once for legacy
// migration from older desktop builds.

import { createContext, useCallback, useContext, useEffect, useState } from "react";
import type { ReactNode } from "react";
import { en, type DictKey } from "../locales/en";
import { zh } from "../locales/zh";
import { zhTW } from "../locales/zh-TW";

export type Locale = "en" | "zh" | "zh-TW";
export type { DictKey };
// LangPref is the stored preference: "" means auto-detect from the OS.
export type LangPref = "" | "en" | "zh" | "zh-TW";

const DICTS: Record<Locale, Record<DictKey, string>> = { en, zh, "zh-TW": zhTW };
const STORAGE_KEY = "reasonix-lang";

// currentLocale mirrors the active locale for callers outside React (lib/tools.ts).
let currentLocale: Locale = "en";

// Whimsical present-participles cycled in the status line while a turn runs. Kept
// out of the dict (it's an array, and purely decorative) but localized all the same.
export const SPINNER_WORDS: Record<Locale, string[]> = {
  en: [
    "Frolicking", "Pondering", "Noodling", "Brewing", "Conjuring", "Cogitating",
    "Percolating", "Ruminating", "Simmering", "Synthesizing", "Tinkering",
    "Marinating", "Crunching", "Hatching", "Mulling", "Whirring", "Forging",
    "Spelunking", "Puttering", "Vibing",
  ],
  zh: [
    "嬉游中", "沉思中", "鼓捣中", "酝酿中", "施法中", "苦思中",
    "渗滤中", "反刍中", "文火慢炖", "合成中", "修补中",
    "腌制入味", "嘎吱运算", "孵化中", "盘算中", "嗡嗡运转", "锻造中",
    "探洞中", "摆弄中", "来感觉了",
  ],
  "zh-TW": [
    "嬉遊中", "沉思中", "鼓搗中", "醞釀中", "施法中", "苦思中",
    "滲濾中", "反芻中", "文火慢燉", "合成中", "修補中",
    "醃製入味", "嘎吱運算", "孵化中", "盤算中", "嗡嗡運轉", "鍛造中",
    "探洞中", "擺弄中", "來感覺了",
  ],
};

export function detectLocale(pref: LangPref): Locale {
  if (pref === "en" || pref === "zh" || pref === "zh-TW") return pref;
  const nav = typeof navigator !== "undefined" ? navigator.language.toLowerCase() : "en";
  if (nav.startsWith("zh-tw") || nav.startsWith("zh-hant") || nav === "zh-hk" || nav === "zh-mo") return "zh-TW";
  return nav.startsWith("zh") ? "zh" : "en";
}

function readPref(): LangPref {
  return "";
}

export function normalizeLangPref(v: unknown): LangPref {
  return v === "en" || v === "zh" || v === "zh-TW" ? v : "";
}

export function readLegacyLangPref(): LangPref {
  const v = typeof localStorage !== "undefined" ? localStorage.getItem(STORAGE_KEY) : null;
  return normalizeLangPref(v);
}

export function clearLegacyLangPref(): void {
  try {
    localStorage.removeItem(STORAGE_KEY);
  } catch {
    /* private mode / no storage */
  }
}

// translate resolves a key for a locale and fills {placeholders}. Missing keys fall
// back to English, then to the raw key, so the UI never renders blank.
function translate(locale: Locale, key: DictKey, vars?: Record<string, string | number>): string {
  const s = DICTS[locale][key] ?? DICTS.en[key] ?? key;
  if (!vars) return s;
  return s.replace(/\{(\w+)\}/g, (_, k) => (vars[k] !== undefined ? String(vars[k]) : `{${k}}`));
}

// t is the non-reactive translator for code outside React (e.g. lib/tools.ts). It
// reads the module mirror, which the provider keeps in sync.
export function t(key: DictKey, vars?: Record<string, string | number>): string {
  return translate(currentLocale, key, vars);
}

export function getLocale(): Locale {
  return currentLocale;
}

export type Translator = (key: DictKey, vars?: Record<string, string | number>) => string;

interface I18nValue {
  locale: Locale;
  pref: LangPref;
  setPref: (pref: LangPref) => void;
  t: Translator;
}

const I18nContext = createContext<I18nValue | null>(null);

export function LocaleProvider({ children }: { children: ReactNode }) {
  const [pref, setPrefState] = useState<LangPref>(() => readPref());
  const locale = detectLocale(pref);
  currentLocale = locale; // keep the mirror fresh for non-React callers

  useEffect(() => {
    if (typeof document === "undefined") return;
    document.documentElement.lang = locale === "zh" ? "zh-CN" : "en";
  }, [locale]);

  // setPref updates only the live UI; persistence is handled by desktop config.
  const setPref = useCallback((next: LangPref) => {
    setPrefState(normalizeLangPref(next));
  }, []);

  const tt = useCallback<Translator>((key, vars) => translate(detectLocale(pref), key, vars), [pref]);

  return <I18nContext.Provider value={{ locale, pref, setPref, t: tt }}>{children}</I18nContext.Provider>;
}

export function useI18n(): I18nValue {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error("useI18n must be used within a LocaleProvider");
  return ctx;
}

// useT is the common shorthand: just the translator.
export function useT(): Translator {
  return useI18n().t;
}
