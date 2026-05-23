import { type Dispatch, type SetStateAction, useState } from "react";

export interface PresetMode {
  /** Canonical preset bucket — `pro` if loop is on v4-pro, otherwise `flash`. */
  preset: "flash" | "pro";
  setPreset: Dispatch<SetStateAction<"flash" | "pro">>;
}

export function usePresetMode(model: string, initialPreset?: "flash" | "pro"): PresetMode {
  const [preset, setPreset] = useState<"flash" | "pro">(
    () => initialPreset ?? (model === "deepseek-v4-pro" ? "pro" : "flash"),
  );
  return { preset, setPreset };
}
