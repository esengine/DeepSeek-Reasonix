import { app } from "../lib/bridge";
import { desktopHost } from "../lib/desktopHost";
import type { BotRuntimeStatusView } from "../lib/types";

export async function loadBotRuntimeStatus(): Promise<BotRuntimeStatusView | null> {
  if (typeof window !== "undefined" && desktopHost().kind === "none") return null;
  try {
    return await app.BotRuntimeStatus();
  } catch (error) {
    console.warn("bot runtime status failed", error);
    return null;
  }
}
