const require_wrapAnsiBg = require("./_internal/wrapAnsiBg.js");
//#region src/server/colors/bgRedBright.ts
/**
* Bright red background. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.bgRedBright('hello'));
*/
function bgRedBright(text) {
	return require_wrapAnsiBg.wrapAnsiBg("\x1B[101m", "\x1B[49m", text);
}
//#endregion
exports.bgRedBright = bgRedBright;
