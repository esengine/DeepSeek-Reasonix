const require_wrapAnsi = require("./_internal/wrapAnsi.js");
//#region src/server/colors/blue.ts
/**
* Blue foreground. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.blue('hello'));
*/
function blue(text) {
	return require_wrapAnsi.wrapAnsi("\x1B[34m", "\x1B[39m", text);
}
//#endregion
exports.blue = blue;
