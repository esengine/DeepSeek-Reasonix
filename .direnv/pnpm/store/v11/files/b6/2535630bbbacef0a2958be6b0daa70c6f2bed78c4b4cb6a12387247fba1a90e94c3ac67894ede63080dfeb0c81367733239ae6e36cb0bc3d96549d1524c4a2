import { wrapAnsi } from "./_internal/wrapAnsi.mjs";
//#region src/server/colors/hidden.ts
/**
* Hides text from rendering while keeping it selectable. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.hidden('hello'));
*/
function hidden(text) {
	return wrapAnsi("\x1B[8m", "\x1B[28m", text);
}
//#endregion
export { hidden };
