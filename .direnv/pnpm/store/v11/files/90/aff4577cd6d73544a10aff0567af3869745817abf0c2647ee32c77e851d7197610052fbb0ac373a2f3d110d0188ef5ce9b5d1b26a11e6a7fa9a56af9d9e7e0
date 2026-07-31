import { toString } from "../util/toString.mjs";
import { toNumber } from "../util/toNumber.mjs";
//#region src/compat/math/add.ts
/**
* Adds two numbers while safely handling `NaN` values.
*
* This function takes two numbers and returns their sum. If either of the numbers is `NaN`,
* the function returns `NaN`.
*
* @param value - The first number to add.
* @param other - The second number to add.
* @returns The sum of the two numbers, or `NaN` if any input is `NaN`.
*
* @example
* const result1 = add(2, 3);    // result1 will be 5
* const result2 = add(5, NaN);  // result2 will be NaN
* const result3 = add(NaN, 10); // result3 will be NaN
*/
function add(value, other) {
	if (value === void 0 && other === void 0) return 0;
	if (value === void 0 || other === void 0) return value ?? other;
	if (typeof value === "string" || typeof other === "string") {
		value = toString(value);
		other = toString(other);
	} else {
		value = toNumber(value);
		other = toNumber(other);
	}
	return value + other;
}
//#endregion
export { add };
