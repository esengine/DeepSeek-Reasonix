const require_wrapAnsi = require("./_internal/wrapAnsi.js");
//#region src/server/colors/reset.ts
/**
* Resets every style and color set up to this point. Wraps text with the ANSI reset code.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.reset('hello'));
*/
function reset(text) {
	return require_wrapAnsi.wrapAnsi("\x1B[0m", "\x1B[0m", text);
}
//#endregion
exports.reset = reset;
