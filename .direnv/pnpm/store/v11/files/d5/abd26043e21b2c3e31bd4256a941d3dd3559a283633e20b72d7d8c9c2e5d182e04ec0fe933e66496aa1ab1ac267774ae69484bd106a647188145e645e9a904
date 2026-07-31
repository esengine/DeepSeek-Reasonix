import { wrapAnsi } from "./_internal/wrapAnsi.mjs";
//#region src/server/colors/dim.ts
/**
* Dim (faint) text. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.dim('hello'));
*/
function dim(text) {
	return wrapAnsi("\x1B[2m", "\x1B[22m", text);
}
//#endregion
export { dim };
