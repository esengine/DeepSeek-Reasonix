const require_wrapAnsiBg = require("./_internal/wrapAnsiBg.js");
//#region src/server/colors/bgBlack.ts
/**
* Black background. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.bgBlack('hello'));
*/
function bgBlack(text) {
	return require_wrapAnsiBg.wrapAnsiBg("\x1B[40m", "\x1B[49m", text);
}
//#endregion
exports.bgBlack = bgBlack;
