const require_wrapAnsiBg = require("./_internal/wrapAnsiBg.js");
//#region src/server/colors/bgRed.ts
/**
* Red background. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.bgRed('hello'));
*/
function bgRed(text) {
	return require_wrapAnsiBg.wrapAnsiBg("\x1B[41m", "\x1B[49m", text);
}
//#endregion
exports.bgRed = bgRed;
