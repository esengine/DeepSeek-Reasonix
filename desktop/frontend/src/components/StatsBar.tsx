import { createContext } from "react";
import type { WireUsage } from "../lib/types";

// ── LiveStats context ──────────────────────────────────────────────────────

export interface LiveStats {
  usage?: WireUsage;
  turnStartAt?: number;
  running: boolean;
}

export const LiveStatsContext = createContext<LiveStats>({
  running: false,
});
