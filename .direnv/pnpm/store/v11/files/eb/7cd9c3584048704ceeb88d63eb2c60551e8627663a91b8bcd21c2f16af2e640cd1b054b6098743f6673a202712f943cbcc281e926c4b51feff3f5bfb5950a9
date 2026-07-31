const require_wrapAnsi = require("./_internal/wrapAnsi.js");
//#region src/server/colors/bold.ts
/**
* Bold text. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.bold('hello'));
*/
function bold(text) {
	return require_wrapAnsi.wrapAnsi("\x1B[1m", "\x1B[22m", text);
}
//#endregion
exports.bold = bold;
