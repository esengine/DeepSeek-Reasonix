const require_toInteger = require("./toInteger.js");
const require_clamp = require("../math/clamp.js");
const require_MAX_SAFE_INTEGER = require("../_internal/MAX_SAFE_INTEGER.js");
//#region src/compat/util/toSafeInteger.ts
/**
* Converts `value` to a safe integer.
*
* A safe integer can be compared and represented correctly.
*
* @param value - The value to convert.
* @returns Returns the value converted to a safe integer.
*
* @example
* toSafeInteger(3.2); // => 3
* toSafeInteger(Number.MAX_VALUE); // => 9007199254740991
* toSafeInteger(Infinity); // => 9007199254740991
* toSafeInteger('3.2'); // => 3
* toSafeInteger(NaN); // => 0
* toSafeInteger(null); // => 0
* toSafeInteger(-Infinity); // => -9007199254740991
*/
function toSafeInteger(value) {
	if (value == null) return 0;
	return require_clamp.clamp(require_toInteger.toInteger(value), -require_MAX_SAFE_INTEGER.MAX_SAFE_INTEGER, require_MAX_SAFE_INTEGER.MAX_SAFE_INTEGER);
}
//#endregion
exports.toSafeInteger = toSafeInteger;
