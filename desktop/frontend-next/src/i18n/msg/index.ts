import { ABOUT } from "./about";
import { APP } from "./app";
import { CHAT } from "./chat";
import { COMMON } from "./common";
import { COMPOSER } from "./composer";
import { DATA } from "./data";
import { EXT } from "./ext";
import { FEEDBACK } from "./feedback";
import { KERNEL } from "./kernel";
import { LOOK } from "./look";
import { MEMORY } from "./memory";
import { MODELS } from "./models";
import { ONBOARDING } from "./onboarding";
import { PANELS } from "./panels";
import { PERSONA } from "./persona";
import { PLAN } from "./plan";
import { REMOTE } from "./remote";
import { SAFETY } from "./safety";
import { SESSION } from "./session";
import { SETTINGS } from "./settings";
import { SUBAGENT } from "./subagent";
import { TERMINAL } from "./terminal";
import { USAGE } from "./usage";
import { WORKBENCH } from "./workbench";
import type { Messages } from "./types";

export type { Message, Messages } from "./types";

/** Every message by its key. Each area's table is one file; a key's first
 *  segment names the file it lives in. */
export const AREAS: Record<string, Messages> = { about: ABOUT, app: APP, chat: CHAT, common: COMMON, composer: COMPOSER, data: DATA, ext: EXT, feedback: FEEDBACK, kernel: KERNEL, look: LOOK, memory: MEMORY, models: MODELS, onboarding: ONBOARDING, panels: PANELS, persona: PERSONA, plan: PLAN, remote: REMOTE, safety: SAFETY, session: SESSION, settings: SETTINGS, subagent: SUBAGENT, terminal: TERMINAL, usage: USAGE, workbench: WORKBENCH };

export const MESSAGES: Messages = Object.assign({}, ...Object.values(AREAS));

/** A key is an area and a meaning in lower-case dotted words. */
export const KEY = /^[a-z][a-z0-9_]*(\.[a-z0-9_]+)+$/;

export const catalogue = (lang: "zh" | "en"): Record<string, string> =>
  Object.fromEntries(Object.entries(MESSAGES).map(([key, m]) => [key, m[lang]]));
