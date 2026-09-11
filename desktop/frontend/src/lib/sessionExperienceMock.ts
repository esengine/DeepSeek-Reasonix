import type { SettingsView } from "./types";

type MockSessionSettings = Pick<SettingsView,
  "sessionExperience" | "displayMode" | "reasoningDisplayMode" | "reasoningDisplayModeExplicit"
>;

export function applyMockSessionExperience(settings: MockSessionSettings, value: unknown): void {
  if (value !== "standard" && value !== "deep") throw new Error("invalid session experience");
  settings.sessionExperience = value;
  settings.displayMode = "standard";
  settings.reasoningDisplayMode = value === "deep" ? "expanded" : "auto";
  settings.reasoningDisplayModeExplicit = true;
}

export function applyMockLegacyReasoningMode(settings: MockSessionSettings, value: string): void {
  applyMockSessionExperience(settings, value === "expanded" ? "deep" : "standard");
}
