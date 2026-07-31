const require_wrapAnsiBg = require("./_internal/wrapAnsiBg.js");
//#region src/server/colors/bgAnsi256.ts
/**
* Returns a function that wraps text with the given 8-bit (256-color) background.
*
* @param code - The 8-bit color code (0–255).
* @returns A color function that wraps text with the chosen ANSI 256 background.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* const warn = colors.bgAnsi256(208);
* console.log(warn('hello'));
*/
function bgAnsi256(code) {
	return (text) => require_wrapAnsiBg.wrapAnsiBg(`\x1b[48;5;${code}m`, "\x1B[49m", text);
}
//#endregion
exports.bgAnsi256 = bgAnsi256;
