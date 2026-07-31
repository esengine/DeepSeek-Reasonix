const require_wrapAnsiBg = require("./_internal/wrapAnsiBg.js");
//#region src/server/colors/bgBlueBright.ts
/**
* Bright blue background. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.bgBlueBright('hello'));
*/
function bgBlueBright(text) {
	return require_wrapAnsiBg.wrapAnsiBg("\x1B[104m", "\x1B[49m", text);
}
//#endregion
exports.bgBlueBright = bgBlueBright;
