const require_decimalAdjust = require("../_internal/decimalAdjust.js");
//#region src/compat/math/ceil.ts
/**
* Computes number rounded up to precision.
*
* @param number The number to round up.
* @param precision The precision to round up to.
* @returns Returns the rounded up number.
*
* @example
* ceil(4.006); // => 5
* ceil(6.004, 2); // => 6.01
* ceil(6040, -2); // => 6100
*/
function ceil(number, precision = 0) {
	return require_decimalAdjust.decimalAdjust("ceil", number, precision);
}
//#endregion
exports.ceil = ceil;
