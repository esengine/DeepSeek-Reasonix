export interface BorderStyle {
  readonly topLeft: string;
  readonly top: string;
  readonly topRight: string;
  readonly left: string;
  readonly right: string;
  readonly bottomLeft: string;
  readonly bottom: string;
  readonly bottomRight: string;
}

export type BorderStyleName = "single" | "double" | "round" | "bold" | "ascii";

const BORDER_PRESETS: Record<BorderStyleName, BorderStyle> = {
  single: {
    topLeft: "┌",
    top: "─",
    topRight: "┐",
    left: "│",
    right: "│",
    bottomLeft: "└",
    bottom: "─",
    bottomRight: "┘",
  },
  double: {
    topLeft: "╔",
    top: "═",
    topRight: "╗",
    left: "║",
    right: "║",
    bottomLeft: "╚",
    bottom: "═",
    bottomRight: "╝",
  },
  round: {
    topLeft: "╭",
    top: "─",
    topRight: "╮",
    left: "│",
    right: "│",
    bottomLeft: "╰",
    bottom: "─",
    bottomRight: "╯",
  },
  bold: {
    topLeft: "┏",
    top: "━",
    topRight: "┓",
    left: "┃",
    right: "┃",
    bottomLeft: "┗",
    bottom: "━",
    bottomRight: "┛",
  },
  ascii: {
    topLeft: "+",
    top: "-",
    topRight: "+",
    left: "|",
    right: "|",
    bottomLeft: "+",
    bottom: "-",
    bottomRight: "+",
  },
};

export function resolveBorderStyle(
  style: BorderStyle | BorderStyleName | undefined,
): BorderStyle | undefined {
  if (style === undefined) return undefined;
  if (typeof style === "string") return BORDER_PRESETS[style];
  return style;
}
