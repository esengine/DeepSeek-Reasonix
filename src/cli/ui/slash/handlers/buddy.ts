import { type ResolvedBuddyConfig, loadBuddyConfig, saveBuddyConfig } from "@/config.js";
import type { SlashHandler } from "../dispatch.js";

const WHALE = "\u{1F40B}";
const USAGE = "Usage: /buddy [on|off|mute|unmute|pet|name <name>|status]";

function statusLine(config: ResolvedBuddyConfig): string {
  const enabled = config.enabled ? "on" : "off";
  const voice = config.muted ? "muted" : "voice on";
  return `${WHALE} Buddy is ${enabled}, ${voice}, name "${config.name}".`;
}

const buddy: SlashHandler = (args, _loop, ctx) => {
  const action = (args[0] ?? "status").toLowerCase();
  const configPath = ctx.configPath;

  const save = (patch: Parameters<typeof saveBuddyConfig>[0]) => {
    const next = saveBuddyConfig(patch, configPath);
    ctx.setBuddyConfig?.(next);
    return next;
  };

  if (action === "status" || action === "") {
    return { info: `${statusLine(loadBuddyConfig(configPath))}\n${USAGE}` };
  }

  if (action === "on" || action === "enable") {
    const next = save({ enabled: true });
    ctx.pulseBuddy?.("wake");
    return { info: `${statusLine(next)}\nThe Reasonix whale is back near the composer.` };
  }

  if (action === "off" || action === "disable") {
    const next = save({ enabled: false });
    return { info: `${statusLine(next)}\nThe Reasonix whale is hidden.` };
  }

  if (action === "mute") {
    const next = save({ muted: true });
    return { info: `${statusLine(next)}\nBuddy will stay visual-only.` };
  }

  if (action === "unmute") {
    const next = save({ muted: false });
    ctx.pulseBuddy?.("wake");
    return { info: `${statusLine(next)}\nBuddy voice is enabled.` };
  }

  if (action === "pet") {
    let next = loadBuddyConfig(configPath);
    if (!next.enabled) next = save({ enabled: true });
    ctx.setBuddyConfig?.(next);
    ctx.pulseBuddy?.("pet");
    return { info: `${WHALE} ${next.name}: bloop.` };
  }

  if (action === "name") {
    const name = args.slice(1).join(" ");
    if (!name.trim()) return { info: `Missing buddy name.\n${USAGE}` };
    const next = save({ name });
    ctx.pulseBuddy?.("wake");
    return { info: `${WHALE} Buddy renamed to "${next.name}".` };
  }

  return { info: `${USAGE}\nUnknown buddy action: ${action}` };
};

export const handlers: Record<string, SlashHandler> = {
  buddy,
};
