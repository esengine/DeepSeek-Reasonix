const require_decimalAdjust = require("../_internal/decimalAdjust.js");
//#region src/compat/math/floor.ts
/**
* Computes number rounded down to precision.
*
* @param number The number to round down.
* @param precision The precision to round down to.
* @returns Returns the rounded down number.
*
* @example
* floor(4.006); // => 4
* floor(0.046, 2); // => 0.04
* floor(4060, -2); // => 4000
*/
function floor(number, precision = 0) {
	return require_decimalAdjust.decimalAdjust("floor", number, precision);
}
//#endregion
exports.floor = floor;
