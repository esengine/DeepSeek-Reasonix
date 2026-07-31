const require_wrapAnsi = require("./_internal/wrapAnsi.js");
//#region src/server/colors/rgb.ts
/**
* Returns a function that wraps text with the given 24-bit (truecolor) foreground.
*
* @param r - Red component (0–255).
* @param g - Green component (0–255).
* @param b - Blue component (0–255).
* @returns A color function that wraps text with the chosen RGB foreground.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* const brand = colors.rgb(255, 99, 71);
* console.log(brand('hello'));
*/
function rgb(r, g, b) {
	return (text) => require_wrapAnsi.wrapAnsi(`\x1b[38;2;${r};${g};${b}m`, "\x1B[39m", text);
}
//#endregion
exports.rgb = rgb;
