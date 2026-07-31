const require_wrapAnsi = require("./_internal/wrapAnsi.js");
//#region src/server/colors/italic.ts
/**
* Italic text. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.italic('hello'));
*/
function italic(text) {
	return require_wrapAnsi.wrapAnsi("\x1B[3m", "\x1B[23m", text);
}
//#endregion
exports.italic = italic;
