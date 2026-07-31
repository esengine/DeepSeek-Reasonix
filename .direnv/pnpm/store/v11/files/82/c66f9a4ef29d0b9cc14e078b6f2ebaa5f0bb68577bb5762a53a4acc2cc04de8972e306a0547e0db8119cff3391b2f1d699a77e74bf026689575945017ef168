import { toString } from "../util/toString.mjs";
import { toNumber } from "../util/toNumber.mjs";
//#region src/compat/math/multiply.ts
/**
* Multiply two numbers.
*
* If either of the numbers is `NaN`, the function returns `NaN`.
*
* @param value The first number in a multiplication
* @param other The second number in a multiplication
* @returns The product of value and other
*
* @example
* multiply(2, 3); // => 6
* multiply(2, NaN); // => NaN
* multiply(NaN, 3); // => NaN
* multiply(NaN, NaN); // => NaN
*/
function multiply(value, other) {
	if (value === void 0 && other === void 0) return 1;
	if (value === void 0 || other === void 0) return value ?? other;
	if (typeof value === "string" || typeof other === "string") {
		value = toString(value);
		other = toString(other);
	} else {
		value = toNumber(value);
		other = toNumber(other);
	}
	return value * other;
}
//#endregion
export { multiply };
