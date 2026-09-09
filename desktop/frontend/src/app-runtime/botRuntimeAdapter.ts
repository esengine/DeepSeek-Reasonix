import { app } from "../lib/bridge";
import type { BotRuntimeStatusView } from "../lib/types";

export async function loadBotRuntimeStatus(): Promise<BotRuntimeStatusView | null> {
  if (typeof window !== "undefined" && !window.runtime) return null;
  try {
    return await app.BotRuntimeStatus();
  } catch (error) {
    console.warn("bot runtime status failed", error);
    return null;
  }
}
