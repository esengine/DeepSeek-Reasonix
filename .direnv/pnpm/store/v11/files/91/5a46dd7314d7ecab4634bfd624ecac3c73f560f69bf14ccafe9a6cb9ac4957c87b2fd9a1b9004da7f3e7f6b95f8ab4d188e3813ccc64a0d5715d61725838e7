const require_escapeRegExp = require("../../string/escapeRegExp.js");
const require_toString = require("../util/toString.js");
//#region src/compat/string/escapeRegExp.ts
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
	return require_escapeRegExp.escapeRegExp(require_toString.toString(str));
}
//#endregion
exports.escapeRegExp = escapeRegExp;
