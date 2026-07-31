import { wrapAnsi } from "./_internal/wrapAnsi.mjs";
//#region src/server/colors/inverse.ts
/**
* Swaps the foreground and background colors. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.inverse('hello'));
*/
function inverse(text) {
	return wrapAnsi("\x1B[7m", "\x1B[27m", text);
}
//#endregion
export { inverse };
