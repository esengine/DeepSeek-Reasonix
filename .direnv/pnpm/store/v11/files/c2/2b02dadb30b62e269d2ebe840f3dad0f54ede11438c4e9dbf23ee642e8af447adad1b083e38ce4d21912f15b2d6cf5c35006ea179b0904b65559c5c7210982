import { inRange as inRange$1 } from "../../math/inRange.mjs";
//#region src/compat/math/inRange.ts
/**
* Checks if the value is within a specified range.
*
* @param value The value to check.
* @param minimum The lower bound of the range (inclusive).
* @param maximum The upper bound of the range (exclusive).
* @returns `true` if the value is within the specified range, otherwise `false`.
* @throws {Error} Throws an error if the `minimum` is greater or equal than the `maximum`.
*
* @example
* const result1 = inRange(3, 5); // result1 will be true.
* const result2 = inRange(1, 2, 5); // result2 will be false.
* const result3 = inRange(1, 5, 2); // If the minimum is greater or equal than the maximum, an error is thrown.
*/
function inRange(value, minimum, maximum) {
	if (!minimum) minimum = 0;
	if (maximum != null && !maximum) maximum = 0;
	if (minimum != null && typeof minimum !== "number") minimum = Number(minimum);
	if (maximum == null && minimum === 0) return false;
	if (maximum != null && typeof maximum !== "number") maximum = Number(maximum);
	if (maximum != null && minimum > maximum) [minimum, maximum] = [maximum, minimum];
	if (minimum === maximum) return false;
	return inRange$1(value, minimum, maximum);
}
//#endregion
export { inRange };
