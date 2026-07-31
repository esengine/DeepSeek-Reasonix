import { getTag } from "../_internal/getTag.mjs";
import "../_internal/tags.mjs";
import { isObjectLike } from "./isObjectLike.mjs";
//#region src/compat/predicate/isNumber.ts
/**
* Checks if a given value is a number.
*
* This function can also serve as a type predicate in TypeScript, narrowing the type of the argument to `number`.
*
* @param value The value to check if it is a number.
* @returns Returns `true` if `value` is a number, else `false`.
*
* @example
* const value1 = 123;
* const value2 = 'abc';
* const value3 = true;
*
* console.log(isNumber(value1)); // true
* console.log(isNumber(value2)); // false
* console.log(isNumber(value3)); // false
*/
function isNumber(value) {
	return typeof value === "number" || isObjectLike(value) && getTag(value) === "[object Number]";
}
//#endregion
export { isNumber };
