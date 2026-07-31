import { ColorFunction } from "./types.mjs";

//#region src/server/colors/hex.d.ts
/**
 * Returns a function that wraps text with a 24-bit foreground color parsed from a hex string.
 * Accepts `#RGB`, `#RRGGBB`, or the same without `#`.
 *
 * @param color - A hex color string (e.g. `"#ff6347"`, `"#f00"`).
 * @returns A color function that wraps text with the parsed RGB foreground.
 *
 * @example
 * import { colors } from 'es-toolkit/server';
 *
 * console.log(colors.hex('#ff6347')('hello'));
 */
declare function hex(color: string): ColorFunction;
//#endregion
export { hex };