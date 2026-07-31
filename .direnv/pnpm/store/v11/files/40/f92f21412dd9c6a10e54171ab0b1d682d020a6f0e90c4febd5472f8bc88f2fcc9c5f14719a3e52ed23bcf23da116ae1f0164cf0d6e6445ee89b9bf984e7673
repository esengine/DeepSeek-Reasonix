const require_wrapAnsi = require("./_internal/wrapAnsi.js");
//#region src/server/colors/underline.ts
/**
* Underlined text. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.underline('hello'));
*/
function underline(text) {
	return require_wrapAnsi.wrapAnsi("\x1B[4m", "\x1B[24m", text);
}
//#endregion
exports.underline = underline;
