import { wrapAnsiBg } from "./_internal/wrapAnsiBg.mjs";
//#region src/server/colors/bgBlackBright.ts
/**
* Bright black (gray) background. Wraps text with ANSI codes.
*
* @param text - The text to style.
* @returns The styled text.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.bgBlackBright('hello'));
*/
function bgBlackBright(text) {
	return wrapAnsiBg("\x1B[100m", "\x1B[49m", text);
}
//#endregion
export { bgBlackBright };
