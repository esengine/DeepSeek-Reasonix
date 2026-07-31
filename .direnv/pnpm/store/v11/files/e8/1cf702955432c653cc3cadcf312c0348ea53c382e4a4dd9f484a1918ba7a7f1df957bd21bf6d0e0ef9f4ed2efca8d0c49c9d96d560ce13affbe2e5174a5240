//#region src/string/escapeRegExp.ts
/**
* Escapes the RegExp special characters "^", "$", "\\", ".", "*", "+", "?", "(", ")", "[", "]", "{", "}", and "|" in `str`.
*
* @param str The string to escape.
* @returns Returns the escaped string.
*
* @example
* import { escapeRegExp } from 'es-toolkit/string';
*
* escapeRegExp('[es-toolkit](https://es-toolkit.dev/)'); // returns '\[es-toolkit\]\(https://es-toolkit\.dev/\)'
*/
function escapeRegExp(str) {
	return str.replace(/[\\^$.*+?()[\]{}|]/g, "\\$&");
}
//#endregion
exports.escapeRegExp = escapeRegExp;
