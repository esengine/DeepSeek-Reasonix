import { toInteger } from "./toInteger.mjs";
import { clamp } from "../math/clamp.mjs";
import { MAX_SAFE_INTEGER } from "../_internal/MAX_SAFE_INTEGER.mjs";
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
	return clamp(toInteger(value), -MAX_SAFE_INTEGER, MAX_SAFE_INTEGER);
}
//#endregion
export { toSafeInteger };
