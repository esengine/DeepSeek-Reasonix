const require_wrapAnsiBg = require("./_internal/wrapAnsiBg.js");
//#region src/server/colors/bgYellow.ts
/**
* Yellow background. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.bgYellow('hello'));
*/
function bgYellow(text) {
	return require_wrapAnsiBg.wrapAnsiBg("\x1B[43m", "\x1B[49m", text);
}
//#endregion
exports.bgYellow = bgYellow;
