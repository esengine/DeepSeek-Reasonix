const require_wrapAnsi = require("./_internal/wrapAnsi.js");
//#region src/server/colors/strikethrough.ts
/**
* Strikethrough text. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.strikethrough('hello'));
*/
function strikethrough(text) {
	return require_wrapAnsi.wrapAnsi("\x1B[9m", "\x1B[29m", text);
}
//#endregion
exports.strikethrough = strikethrough;
