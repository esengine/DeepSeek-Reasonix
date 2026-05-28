/** Bg-tinted inline chip — section labels (REASONING / TASK / TOOL) and badges (model / path). */

import { type Color, Text } from "ink";
// biome-ignore lint/style/useImportType: tsconfig jsx=react needs React in value scope for JSX compilation
import React from "react";
import { PILL } from "../theme/tokens.js";

export interface PillProps {
  label: string;
  bg: Color;
  fg: Color;
  bold?: boolean;
}

export function Pill({ label, bg, fg, bold = true }: PillProps): React.ReactElement {
  return <Text backgroundColor={bg} color={fg} bold={bold}>{` ${label} `}</Text>;
}

/** Section pill colors — derived from active theme via PILL proxy. */
export function pillSection(): typeof PILL.section {
  return PILL.section;
}

/** Path pill — theme-aware bg-elev tint for filenames / paths. */
export function pillPath(): typeof PILL.path {
  return PILL.path;
}

/** Model pill — theme-aware neutral bg, color signals model class. */
export function pillModel(): typeof PILL.model {
  return PILL.model;
}

export interface ModelBadge {
  label: string;
  kind: keyof typeof PILL.model;
}

/** Map full DeepSeek model id to short label + color class. */
export function modelBadgeFor(model: string | undefined): ModelBadge {
  if (!model) return { label: "?", kind: "unknown" };
  const stripped = model.replace(/^deepseek-/, "");
  if (stripped === "v4-flash" || stripped === "chat") return { label: "v4-flash", kind: "flash" };
  if (stripped === "v4-pro") return { label: "v4-pro", kind: "pro" };
  if (stripped === "r1" || stripped === "reasoner") return { label: "r1", kind: "r1" };
  return { label: stripped, kind: "unknown" };
}
