import { COMMON } from "./common";
import { PLAN } from "./plan";
import { SUBAGENT } from "./subagent";
import type { Messages } from "./types";

export type { Message, Messages } from "./types";

/** Every message by its key. Each area's table is one file; a key's first
 *  segment names the file it lives in. */
export const AREAS: Record<string, Messages> = { common: COMMON, plan: PLAN, subagent: SUBAGENT };

export const MESSAGES: Messages = Object.assign({}, ...Object.values(AREAS));

/** A key is an area and a meaning in lower-case dotted words. */
export const KEY = /^[a-z][a-z0-9_]*(\.[a-z0-9_]+)+$/;

export const catalogue = (lang: "zh" | "en"): Record<string, string> =>
  Object.fromEntries(Object.entries(MESSAGES).map(([key, m]) => [key, m[lang]]));
