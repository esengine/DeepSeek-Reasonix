const require_wrapAnsiBg = require("./_internal/wrapAnsiBg.js");
//#region src/server/colors/bgYellowBright.ts
/**
* Bright yellow background. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.bgYellowBright('hello'));
*/
function bgYellowBright(text) {
	return require_wrapAnsiBg.wrapAnsiBg("\x1B[103m", "\x1B[49m", text);
}
//#endregion
exports.bgYellowBright = bgYellowBright;
