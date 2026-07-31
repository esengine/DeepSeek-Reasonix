const require_wrapAnsiBg = require("./_internal/wrapAnsiBg.js");
//#region src/server/colors/bgWhiteBright.ts
/**
* Bright white background. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.bgWhiteBright('hello'));
*/
function bgWhiteBright(text) {
	return require_wrapAnsiBg.wrapAnsiBg("\x1B[107m", "\x1B[49m", text);
}
//#endregion
exports.bgWhiteBright = bgWhiteBright;
