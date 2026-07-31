const require_toString = require("../util/toString.js");
//#region src/compat/string/replace.ts
/**
* Replaces the matched pattern with the replacement string.
*
* @param {} target - The target string.
* @param {} pattern - The pattern to match.
* @param {} replacement - The replacement string or a function that returns the replacement string.
* @returns The new string with the matched pattern replaced.
*
* @example
* replace('abcde', 'de', '123'); // 'abc123'
* replace('abcde', /[bd]/g, '-'); // 'a-c-e'
* replace('abcde', 'de', substring => substring.toUpperCase()); // 'abcDE'
* replace('abcde', /[bd]/g, substring => substring.toUpperCase()); // 'aBcDe'
*/
function replace(target, pattern, replacement) {
	if (arguments.length < 3) return require_toString.toString(target);
	return require_toString.toString(target).replace(pattern, replacement);
}
//#endregion
exports.replace = replace;
