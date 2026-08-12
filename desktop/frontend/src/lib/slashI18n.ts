// slashI18n localizes slash-command descriptions and argument hints in the
// desktop UI. Command descriptions normally arrive from the Go backend, where
// builtin ones are already localized via the CLI i18n catalog — but skills
// (/explore, /research, …) and argument hints (/goal status, /mcp add, …) are
// authored in English and never pass through it. This module overlays the
// frontend dict (locales/en|zh|zh-TW) on top of whatever the backend sent:
// when a `slash.cmd.<name>` / `slash.arg.<cmd>.<label>` key exists it wins,
// otherwise the backend-provided string is shown unchanged (user skills,
// custom commands, and MCP prompts keep their own text).

import { en } from "../locales/en";
import type { DictKey, Translator } from "./i18n";
import type { CommandInfo } from "./types";

// Command names whose descriptions ship with the app (builtin actions +
// builtin skills). Everything else — custom commands, user/project skills,
// MCP prompts — is user-authored content and stays as the backend sent it.
const localizedCommandNames = new Set([
  "new", "clear", "compact", "model", "provider", "effort",
  "memory", "migrate", "goal", "remember", "mcp", "hooks",
  "plugins", "theme", "skill", "reload-cmd", "docs",
  "init", "explore", "research", "install-capability", "review", "security-review", "test",
]);

/** Localized description for a slash command, falling back to the backend text. */
export function localizedCommandDescription(command: CommandInfo, t: Translator): string {
  if (!localizedCommandNames.has(command.name)) return command.description;
  const key = `slash.cmd.${command.name}` as DictKey;
  if (!(key in en)) return command.description;
  return t(key);
}

/**
 * Localized hint for a slash argument item (e.g. "/goal status" → "show active
 * goal and budget runtime"). `commandName` is the slash word without the "/";
 * when the command is unknown or the label has no key, the backend hint is
 * returned unchanged.
 */
export function localizedArgHint(commandName: string, label: string, t: Translator): string | null {
  const key = `slash.arg.${commandName}.${label}` as DictKey;
  if (!(key in en)) return null;
  return t(key);
}
