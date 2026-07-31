import { decimalAdjust } from "../_internal/decimalAdjust.mjs";
//#region src/compat/math/round.ts
/**
* Computes number rounded to precision.
*
* @param number  The number to round.
* @param precision The precision to round to.
* @returns Returns the rounded number.
*
* @example
* round(4.006); // => 4
* round(4.006, 2); // => 4.01
* round(4060, -2); // => 4100
*/
function round(number, precision = 0) {
	return decimalAdjust("round", number, precision);
}
//#endregion
export { round };
