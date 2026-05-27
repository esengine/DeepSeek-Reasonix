export type QQRemoteDesktopCommand =
  | { kind: "help" }
  | { kind: "new" }
  | { kind: "abort" }
  | { kind: "compact" }
  | { kind: "btw"; text: string }
  | { kind: "skill"; name: string; args?: string };

export function parseQQRemoteDesktopCommand(
  text: string,
  skillNames: Iterable<string>,
): QQRemoteDesktopCommand | null {
  const trimmed = text.trim();
  if (!trimmed.startsWith("/")) return null;

  if (trimmed === "/help") return { kind: "help" };
  if (trimmed === "/new") return { kind: "new" };
  if (trimmed === "/abort") return { kind: "abort" };
  if (trimmed === "/compact") return { kind: "compact" };

  const btwMatch = /^\/btw(?:\s+([\s\S]+))?$/.exec(trimmed);
  if (btwMatch) {
    const question = btwMatch[1]?.trim() ?? "";
    return question ? { kind: "btw", text: question } : null;
  }

  const skillMatch = /^\/([a-zA-Z0-9_-]+)(?:\s+([\s\S]+))?$/.exec(trimmed);
  if (!skillMatch) return null;
  const [, rawName, rawArgs] = skillMatch;
  if (!rawName) return null;
  if (rawName === "help" || rawName === "new" || rawName === "abort" || rawName === "compact") {
    return null;
  }
  const names = new Set(skillNames);
  if (!names.has(rawName)) return null;
  const args = rawArgs?.trim() ?? "";
  return { kind: "skill", name: rawName, args: args || undefined };
}

export function qqRemoteDesktopHelpText(skillNames: Iterable<string>): string {
  const skills = [...new Set(skillNames)].sort();
  const skillHint =
    skills.length > 0 ? `\n- /<skill> [args] (available: ${skills.join(", ")})` : "";
  return [
    "QQ remote desktop commands:",
    "- /help",
    "- /new",
    "- /abort",
    "- /compact",
    "- /btw <question>",
    `${skillHint}`.trimEnd(),
    "",
    "UI-only desktop commands stay local.",
  ]
    .filter(Boolean)
    .join("\n");
}

export function qqRemoteCommandBypassesBusy(cmd: QQRemoteDesktopCommand): boolean {
  return cmd.kind === "help" || cmd.kind === "new" || cmd.kind === "abort";
}
