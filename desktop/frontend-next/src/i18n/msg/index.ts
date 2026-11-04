import { CHAT } from "./chat";
import { COMMON } from "./common";
import { COMPOSER } from "./composer";
import { FEEDBACK } from "./feedback";
import { PLAN } from "./plan";
import { REMOTE } from "./remote";
import { SESSION } from "./session";
import { SUBAGENT } from "./subagent";
import { WORKBENCH } from "./workbench";
import type { Messages } from "./types";

export type { Message, Messages } from "./types";

/** Every message by its key. Each area's table is one file; a key's first
 *  segment names the file it lives in. */
export const AREAS: Record<string, Messages> = { chat: CHAT, common: COMMON, composer: COMPOSER, feedback: FEEDBACK, plan: PLAN, remote: REMOTE, session: SESSION, subagent: SUBAGENT, workbench: WORKBENCH };

export const MESSAGES: Messages = Object.assign({}, ...Object.values(AREAS));

/** A key is an area and a meaning in lower-case dotted words. */
export const KEY = /^[a-z][a-z0-9_]*(\.[a-z0-9_]+)+$/;

export const catalogue = (lang: "zh" | "en"): Record<string, string> =>
  Object.fromEntries(Object.entries(MESSAGES).map(([key, m]) => [key, m[lang]]));
