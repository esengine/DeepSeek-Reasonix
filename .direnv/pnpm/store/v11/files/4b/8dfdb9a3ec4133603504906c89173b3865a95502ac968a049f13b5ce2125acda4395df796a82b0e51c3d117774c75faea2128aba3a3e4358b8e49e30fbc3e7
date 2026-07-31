const require_toString = require("../util/toString.js");
//#region src/compat/math/parseInt.ts
/**
* Converts `string` to an integer of the specified radix. If `radix` is undefined or 0, a `radix` of 10 is used unless `string` is a hexadecimal, in which case a `radix` of 16 is used.
*
* @param string The string to convert to an integer.
* @param radix The radix to use when converting the string to an integer. Defaults to `0`.
* @param guard Enables use as an iteratee for methods like `Array#map`.
* @returns Returns the converted integer.
*
* @example
* parseInt('08'); // => 8
* parseInt('0x20'); // => 32
*
* parseInt('08', 10); // => 8
* parseInt('0x20', 16); // => 32
*
* ['6', '08', '10'].map(parseInt); // => [6, 8, 10]
*/
function parseInt(string, radix = 0, guard) {
	if (guard) radix = 0;
	return Number.parseInt(require_toString.toString(string), radix);
}
//#endregion
exports.parseInt = parseInt;
