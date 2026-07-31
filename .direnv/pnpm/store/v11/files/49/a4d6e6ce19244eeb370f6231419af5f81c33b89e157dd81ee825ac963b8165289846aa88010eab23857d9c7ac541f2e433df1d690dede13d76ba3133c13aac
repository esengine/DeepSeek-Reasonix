import { wrapAnsi } from "./_internal/wrapAnsi.mjs";
//#region src/server/colors/strikethrough.ts
/**
* Strikethrough text. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.strikethrough('hello'));
*/
function strikethrough(text) {
	return wrapAnsi("\x1B[9m", "\x1B[29m", text);
}
//#endregion
export { strikethrough };
