import chalk, { type ChalkInstance } from 'chalk';
import type { Color, TextStyles } from './styles.js';

/** Adjust chalk's color level for environments it can't auto-detect. */

const VSCODE_PROGRAM = 'vscode';
const TMUX_TRUECOLOR_OPT_OUT = 'REASONIX_TMUX_TRUECOLOR';

function liftLevelForXtermJs(): boolean {
  if (process.env.TERM_PROGRAM === VSCODE_PROGRAM && chalk.level === 2) {
    chalk.level = 3;
    return true;
  }
  return false;
}

function clampLevelForTmux(): boolean {
  if (process.env[TMUX_TRUECOLOR_OPT_OUT]) return false;
  if (process.env.TMUX && chalk.level > 2) {
    chalk.level = 2;
    return true;
  }
  return false;
}

export const CHALK_BOOSTED_FOR_XTERMJS = liftLevelForXtermJs();
export const CHALK_CLAMPED_FOR_TMUX = clampLevelForTmux();

export type ColorType = 'foreground' | 'background';

const RGB_REGEX = /^rgb\(\s?(\d+),\s?(\d+),\s?(\d+)\s?\)$/;
const ANSI256_REGEX = /^ansi256\(\s?(\d+)\s?\)$/;
const ANSI_PREFIX = 'ansi:';

/** Map of ANSI palette names → `[foreground, background]` chalk applicators. */
const ANSI_PALETTE: Record<string, readonly [ChalkInstance, ChalkInstance]> = {
  black: [chalk.black, chalk.bgBlack],
  red: [chalk.red, chalk.bgRed],
  green: [chalk.green, chalk.bgGreen],
  yellow: [chalk.yellow, chalk.bgYellow],
  blue: [chalk.blue, chalk.bgBlue],
  magenta: [chalk.magenta, chalk.bgMagenta],
  cyan: [chalk.cyan, chalk.bgCyan],
  white: [chalk.white, chalk.bgWhite],
  blackBright: [chalk.blackBright, chalk.bgBlackBright],
  redBright: [chalk.redBright, chalk.bgRedBright],
  greenBright: [chalk.greenBright, chalk.bgGreenBright],
  yellowBright: [chalk.yellowBright, chalk.bgYellowBright],
  blueBright: [chalk.blueBright, chalk.bgBlueBright],
  magentaBright: [chalk.magentaBright, chalk.bgMagentaBright],
  cyanBright: [chalk.cyanBright, chalk.bgCyanBright],
  whiteBright: [chalk.whiteBright, chalk.bgWhiteBright],
};

function applyPaletteColor(text: string, name: string, type: ColorType): string {
  const entry = ANSI_PALETTE[name];
  if (!entry) return text;
  return entry[type === 'foreground' ? 0 : 1](text);
}

function applyAnsi256(text: string, raw: string, type: ColorType): string {
  const match = ANSI256_REGEX.exec(raw);
  if (!match) return text;
  const index = Number(match[1]);
  return type === 'foreground' ? chalk.ansi256(index)(text) : chalk.bgAnsi256(index)(text);
}

function applyRgb(text: string, raw: string, type: ColorType): string {
  const match = RGB_REGEX.exec(raw);
  if (!match) return text;
  const r = Number(match[1]);
  const g = Number(match[2]);
  const b = Number(match[3]);
  return type === 'foreground' ? chalk.rgb(r, g, b)(text) : chalk.bgRgb(r, g, b)(text);
}

/** Wrap `text` in the SGR escapes for `color`. */
export const colorize = (
  text: string,
  color: string | undefined,
  type: ColorType,
): string => {
  if (!color) return text;

  if (color.startsWith(ANSI_PREFIX)) {
    return applyPaletteColor(text, color.slice(ANSI_PREFIX.length), type);
  }

  if (color.startsWith('#')) {
    return type === 'foreground' ? chalk.hex(color)(text) : chalk.bgHex(color)(text);
  }

  if (color.startsWith('ansi256')) {
    return applyAnsi256(text, color, type);
  }

  if (color.startsWith('rgb')) {
    return applyRgb(text, color, type);
  }

  return text;
};

/** Apply a full `TextStyles` block to a string. */
export function applyTextStyles(text: string, styles: TextStyles): string {
  let out = text;

  if (styles.inverse) out = chalk.inverse(out);
  if (styles.strikethrough) out = chalk.strikethrough(out);
  if (styles.underline) out = chalk.underline(out);
  if (styles.italic) out = chalk.italic(out);
  if (styles.bold) out = chalk.bold(out);
  if (styles.dim) out = chalk.dim(out);

  if (styles.color) {
    out = colorize(out, styles.color, 'foreground');
  }
  if (styles.backgroundColor) {
    out = colorize(out, styles.backgroundColor, 'background');
  }

  return out;
}

/** Convenience wrapper that applies a foreground colour only. */
export function applyColor(text: string, color: Color | undefined): string {
  if (!color) return text;
  return colorize(text, color, 'foreground');
}
