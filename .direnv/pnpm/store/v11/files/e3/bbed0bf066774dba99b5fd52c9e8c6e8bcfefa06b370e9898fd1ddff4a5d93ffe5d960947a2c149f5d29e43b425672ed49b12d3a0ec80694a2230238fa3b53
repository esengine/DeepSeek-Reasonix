import { wrapAnsi } from "./_internal/wrapAnsi.mjs";
//#region src/server/colors/ansi256.ts
/**
* Returns a function that wraps text with the given 8-bit (256-color) foreground.
*
* @param code - The 8-bit color code (0–255).
* @returns A color function that wraps text with the chosen ANSI 256 foreground.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* const orange = colors.ansi256(208);
* console.log(orange('hello'));
*/
function ansi256(code) {
	return (text) => wrapAnsi(`\x1b[38;5;${code}m`, "\x1B[39m", text);
}
//#endregion
export { ansi256 };
