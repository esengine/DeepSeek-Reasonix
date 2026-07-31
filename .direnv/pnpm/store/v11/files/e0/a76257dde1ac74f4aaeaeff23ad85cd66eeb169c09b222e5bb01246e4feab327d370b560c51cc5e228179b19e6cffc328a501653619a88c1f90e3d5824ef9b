const require_wrapAnsiBg = require("./_internal/wrapAnsiBg.js");
const require_parseHex = require("./_internal/parseHex.js");
//#region src/server/colors/bgHex.ts
/**
* Returns a function that wraps text with a 24-bit background color parsed from a hex string.
* Accepts `#RGB`, `#RRGGBB`, or the same without `#`.
*
* @param color - A hex color string (e.g. `"#ff6347"`, `"#f00"`).
* @returns A color function that wraps text with the parsed RGB background.
*
* @example
* import { colors } from 'es-toolkit/server';
*
* console.log(colors.bgHex('#ff6347')('hello'));
*/
function bgHex(color) {
	const [r, g, b] = require_parseHex.parseHex(color);
	return (text) => require_wrapAnsiBg.wrapAnsiBg(`\x1b[48;2;${r};${g};${b}m`, "\x1B[49m", text);
}
//#endregion
exports.bgHex = bgHex;
