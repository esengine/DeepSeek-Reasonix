import type { EditMode, ReasoningEffort } from "./protocol";

export const SLASH_EDIT_MODES = [
  "plan",
  "review",
  "auto",
  "yolo",
] as const satisfies readonly EditMode[];
export const SLASH_REASONING_EFFORTS = [
  "low",
  "medium",
  "high",
  "max",
] as const satisfies readonly ReasoningEffort[];

export type SlashSettingsCommand =
  | { type: "editMode"; editMode: EditMode }
  | { type: "reasoningEffort"; reasoningEffort: ReasoningEffort };

export type SlashSettingsDescriptor = {
  cmd: string;
  action: SlashSettingsCommand;
};

function isEditMode(value: string): value is EditMode {
  return (SLASH_EDIT_MODES as readonly string[]).includes(value);
}

function isReasoningEffort(value: string): value is ReasoningEffort {
  return (SLASH_REASONING_EFFORTS as readonly string[]).includes(value);
}

export function parseSlashSettingsCommand(input: string): SlashSettingsCommand | null {
  const match = /^\/([a-zA-Z0-9_-]+)(?:\s+([^\s]+))?$/.exec(input.trim());
  if (!match) return null;

  const name = match[1]?.toLowerCase();
  const arg = match[2]?.toLowerCase();

  if (name === "effort") {
    if (arg && isReasoningEffort(arg)) return { type: "reasoningEffort", reasoningEffort: arg };
    return null;
  }

  if (name === "plan" && !arg) {
    return { type: "editMode", editMode: "plan" };
  }

  if ((name === "model" || name === "mode" || name === "plan") && arg && isEditMode(arg)) {
    return { type: "editMode", editMode: arg };
  }

  return null;
}

export function buildSlashSettingsDescriptors(): SlashSettingsDescriptor[] {
  const modelModes = SLASH_EDIT_MODES.map((mode) => ({
    cmd: `/model ${mode}`,
    action: { type: "editMode", editMode: mode } as const,
  }));
  const planModes = [
    { cmd: "/plan", action: { type: "editMode", editMode: "plan" } as const },
    ...SLASH_EDIT_MODES.filter((mode) => mode !== "plan").map((mode) => ({
      cmd: `/plan ${mode}`,
      action: { type: "editMode", editMode: mode } as const,
    })),
  ];
  const effortCommands = SLASH_REASONING_EFFORTS.map((effort) => ({
    cmd: `/effort ${effort}`,
    action: { type: "reasoningEffort", reasoningEffort: effort } as const,
  }));

  return [...modelModes, ...planModes, ...effortCommands];
}
