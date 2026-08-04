// Русский UI-текст. Набор ключей должен точно совпадать с en.ts — аннотация
// `Record<DictKey, string>` проверяет это на этапе компиляции. Пока переводы
// не готовы, значения берутся из en.ts (fallback), затем заменяются по мере
// перевода.

import { en } from "./en";
import type { DictKey } from "./en";

export const ru: Record<DictKey, string> = { ...en };
