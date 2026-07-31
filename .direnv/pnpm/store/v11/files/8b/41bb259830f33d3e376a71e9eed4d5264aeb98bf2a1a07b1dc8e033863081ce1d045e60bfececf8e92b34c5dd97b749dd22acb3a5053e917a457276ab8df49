import { wrapAnsi } from "./_internal/wrapAnsi.mjs";
//#region src/server/colors/reset.ts
/**
* Resets every style and color set up to this point. Wraps text with the ANSI reset code.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.reset('hello'));
*/
function reset(text) {
	return wrapAnsi("\x1B[0m", "\x1B[0m", text);
}
//#endregion
export { reset };
