export type QQRemoteDesktopCommand =
  | { kind: "help" }
  | { kind: "new" }
  | { kind: "abort" }
  | { kind: "compact" }
  | { kind: "retry" }
  | { kind: "model"; value?: string }
  | { kind: "effort"; value?: "low" | "medium" | "high" | "max" }
  | { kind: "plan"; value?: "review" | "auto" | "yolo" }
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
  if (trimmed === "/retry") return { kind: "retry" };

  const modelMatch = /^\/model(?:\s+([\s\S]+))?$/.exec(trimmed);
  if (modelMatch) {
    const value = modelMatch[1]?.trim() ?? "";
    return { kind: "model", value: value || undefined };
  }

  const effortMatch = /^\/effort(?:\s+(low|medium|high|max))?$/i.exec(trimmed);
  if (effortMatch) {
    const value = effortMatch[1]?.trim().toLowerCase() as
      | "low"
      | "medium"
      | "high"
      | "max"
      | undefined;
    return { kind: "effort", value };
  }

  const planMatch = /^\/plan(?:\s+(review|auto|yolo))?$/i.exec(trimmed);
  if (planMatch) {
    const value = planMatch[1]?.trim().toLowerCase() as "review" | "auto" | "yolo" | undefined;
    return { kind: "plan", value };
  }

  const btwMatch = /^\/btw(?:\s+([\s\S]+))?$/.exec(trimmed);
  if (btwMatch) {
    const question = btwMatch[1]?.trim() ?? "";
    return question ? { kind: "btw", text: question } : null;
  }

  const skillMatch = /^\/([a-zA-Z0-9_-]+)(?:\s+([\s\S]+))?$/.exec(trimmed);
  if (!skillMatch) return null;
  const [, rawName, rawArgs] = skillMatch;
  if (!rawName) return null;
  if (
    rawName === "help" ||
    rawName === "new" ||
    rawName === "abort" ||
    rawName === "compact" ||
    rawName === "retry" ||
    rawName === "model" ||
    rawName === "effort" ||
    rawName === "plan"
  ) {
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
    "- /retry",
    "- /model <flash|pro|deepseek-v4-flash|deepseek-v4-pro>",
    "- /effort <low|medium|high|max>",
    "- /plan <review|auto|yolo>",
    "- /btw <question>",
    `${skillHint}`.trimEnd(),
    "",
    "UI-only desktop commands stay local.",
  ]
    .filter(Boolean)
    .join("\n");
}

export function qqRemoteCommandBypassesBusy(cmd: QQRemoteDesktopCommand): boolean {
  return cmd.kind === "help" || cmd.kind === "new" || cmd.kind === "abort" || cmd.kind === "effort";
}
