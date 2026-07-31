const require_wrapAnsi = require("./_internal/wrapAnsi.js");
//#region src/server/colors/blackBright.ts
/**
* Bright black (gray) foreground. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.blackBright('hello'));
*/
function blackBright(text) {
	return require_wrapAnsi.wrapAnsi("\x1B[90m", "\x1B[39m", text);
}
//#endregion
exports.blackBright = blackBright;
