const require_wrapAnsi = require("./_internal/wrapAnsi.js");
//#region src/server/colors/black.ts
/**
* Black foreground. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.black('hello'));
*/
function black(text) {
	return require_wrapAnsi.wrapAnsi("\x1B[30m", "\x1B[39m", text);
}
//#endregion
exports.black = black;
