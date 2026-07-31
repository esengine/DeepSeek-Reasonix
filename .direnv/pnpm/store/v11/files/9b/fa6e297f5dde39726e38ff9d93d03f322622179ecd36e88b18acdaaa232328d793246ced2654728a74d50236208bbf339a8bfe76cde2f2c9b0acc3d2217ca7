import { wrapAnsiBg } from "./_internal/wrapAnsiBg.mjs";
//#region src/server/colors/bgWhite.ts
/**
* White background. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.bgWhite('hello'));
*/
function bgWhite(text) {
	return wrapAnsiBg("\x1B[47m", "\x1B[49m", text);
}
//#endregion
export { bgWhite };
