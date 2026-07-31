const require_wrapAnsi = require("./_internal/wrapAnsi.js");
//#region src/server/colors/dim.ts
/**
* Dim (faint) text. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.dim('hello'));
*/
function dim(text) {
	return require_wrapAnsi.wrapAnsi("\x1B[2m", "\x1B[22m", text);
}
//#endregion
exports.dim = dim;
