const require_wrapAnsiBg = require("./_internal/wrapAnsiBg.js");
//#region src/server/colors/bgRgb.ts
/**
* Returns a function that wraps text with the given 24-bit (truecolor) background.
*
* @param r - Red component (0–255).
* @param g - Green component (0–255).
* @param b - Blue component (0–255).
* @returns A color function that wraps text with the chosen RGB background.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* const highlight = colors.bgRgb(255, 99, 71);
* console.log(highlight('hello'));
*/
function bgRgb(r, g, b) {
	return (text) => require_wrapAnsiBg.wrapAnsiBg(`\x1b[48;2;${r};${g};${b}m`, "\x1B[49m", text);
}
//#endregion
exports.bgRgb = bgRgb;
