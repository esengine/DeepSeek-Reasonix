import { wrapAnsi } from "./_internal/wrapAnsi.mjs";
import { parseHex } from "./_internal/parseHex.mjs";
//#region src/server/colors/hex.ts
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
function hex(color) {
	const [r, g, b] = parseHex(color);
	return (text) => wrapAnsi(`\x1b[38;2;${r};${g};${b}m`, "\x1B[39m", text);
}
//#endregion
export { hex };
