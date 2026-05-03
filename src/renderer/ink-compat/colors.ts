import type { AnsiCode } from "../pools/style-pool.js";

export type ColorName =
  | "black"
  | "red"
  | "green"
  | "yellow"
  | "blue"
  | "magenta"
  | "cyan"
  | "white"
  | "gray"
  | "grey"
  | "blackBright"
  | "redBright"
  | "greenBright"
  | "yellowBright"
  | "blueBright"
  | "magentaBright"
  | "cyanBright"
  | "whiteBright";

const FG_BASE: Record<ColorName, number> = {
  black: 30,
  red: 31,
  green: 32,
  yellow: 33,
  blue: 34,
  magenta: 35,
  cyan: 36,
  white: 37,
  gray: 90,
  grey: 90,
  blackBright: 90,
  redBright: 91,
  greenBright: 92,
  yellowBright: 93,
  blueBright: 94,
  magentaBright: 95,
  cyanBright: 96,
  whiteBright: 97,
};

const FG_RESET = 39;
const BG_RESET = 49;

export function fgCode(color: string | undefined): AnsiCode | null {
  if (!color) return null;
  if (color.startsWith("#")) {
    const rgb = parseHex(color);
    if (!rgb) return null;
    return { apply: `\x1b[38;2;${rgb[0]};${rgb[1]};${rgb[2]}m`, revert: `\x1b[${FG_RESET}m` };
  }
  const code = FG_BASE[color as ColorName];
  if (code === undefined) return null;
  return { apply: `\x1b[${code}m`, revert: `\x1b[${FG_RESET}m` };
}

export function bgCode(color: string | undefined): AnsiCode | null {
  if (!color) return null;
  if (color.startsWith("#")) {
    const rgb = parseHex(color);
    if (!rgb) return null;
    return { apply: `\x1b[48;2;${rgb[0]};${rgb[1]};${rgb[2]}m`, revert: `\x1b[${BG_RESET}m` };
  }
  const base = FG_BASE[color as ColorName];
  if (base === undefined) return null;
  const bg = base < 90 ? base + 10 : base + 10;
  return { apply: `\x1b[${bg}m`, revert: `\x1b[${BG_RESET}m` };
}

function parseHex(hex: string): [number, number, number] | null {
  const s = hex.slice(1);
  if (s.length === 3) {
    const r = Number.parseInt(s[0]! + s[0]!, 16);
    const g = Number.parseInt(s[1]! + s[1]!, 16);
    const b = Number.parseInt(s[2]! + s[2]!, 16);
    return Number.isNaN(r) ? null : [r, g, b];
  }
  if (s.length === 6) {
    const r = Number.parseInt(s.slice(0, 2), 16);
    const g = Number.parseInt(s.slice(2, 4), 16);
    const b = Number.parseInt(s.slice(4, 6), 16);
    return Number.isNaN(r) ? null : [r, g, b];
  }
  return null;
}
