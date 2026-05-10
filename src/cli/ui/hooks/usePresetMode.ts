import { type Dispatch, type SetStateAction, useState } from "react";

export interface PresetMode {
  /** Canonical preset bucket — `pro` if loop is on v4-pro, otherwise `auto`/`flash` (set by the dashboard's `applyPresetLive`). */
  preset: "auto" | "flash" | "pro";
  setPreset: Dispatch<SetStateAction<"auto" | "flash" | "pro">>;
  /** `/pro` armed → next turn runs on v4-pro. State (rather than reading `loop.proArmed`) so toggles trigger StatsPanel re-render. */
  proArmed: boolean;
  setProArmed: Dispatch<SetStateAction<boolean>>;
  /** True for the duration of a turn that ran on v4-pro because of /pro arming or `⇧ pro` auto-escalation. */
  turnOnPro: boolean;
  setTurnOnPro: Dispatch<SetStateAction<boolean>>;
}

export function usePresetMode(model: string): PresetMode {
  const [preset, setPreset] = useState<"auto" | "flash" | "pro">(() =>
    model === "deepseek-v4-pro" ? "pro" : "auto",
  );
  const [proArmed, setProArmed] = useState(false);
  const [turnOnPro, setTurnOnPro] = useState(false);
  return { preset, setPreset, proArmed, setProArmed, turnOnPro, setTurnOnPro };
}
