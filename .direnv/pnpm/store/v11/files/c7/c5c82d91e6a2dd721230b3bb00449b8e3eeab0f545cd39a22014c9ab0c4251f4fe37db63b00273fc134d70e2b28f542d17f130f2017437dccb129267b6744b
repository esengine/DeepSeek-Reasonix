import { wrapAnsi } from "./_internal/wrapAnsi.mjs";
//#region src/server/colors/white.ts
/**
* White foreground. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.white('hello'));
*/
function white(text) {
	return wrapAnsi("\x1B[37m", "\x1B[39m", text);
}
//#endregion
export { white };
