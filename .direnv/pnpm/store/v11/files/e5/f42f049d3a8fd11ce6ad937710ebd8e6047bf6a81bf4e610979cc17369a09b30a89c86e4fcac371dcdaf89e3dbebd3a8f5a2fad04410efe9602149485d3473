const require_wrapAnsi = require("./_internal/wrapAnsi.js");
//#region src/server/colors/red.ts
/**
* Red foreground. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.red('error'));
*/
function red(text) {
	return require_wrapAnsi.wrapAnsi("\x1B[31m", "\x1B[39m", text);
}
//#endregion
exports.red = red;
