import { wrapAnsiBg } from "./_internal/wrapAnsiBg.mjs";
//#region src/server/colors/bgRed.ts
/**
* Red background. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.bgRed('hello'));
*/
function bgRed(text) {
	return wrapAnsiBg("\x1B[41m", "\x1B[49m", text);
}
//#endregion
export { bgRed };
