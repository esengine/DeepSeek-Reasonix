import { isPlainObject } from "./isPlainObject.mjs";
//#region src/predicate/isEmptyObject.ts
/**
* Checks if a value is an empty plain object.
*
* @param value - The value to check.
* @returns True if the value is an empty plain object, otherwise false.
*
* @example
* isEmptyObject({}); // true
* isEmptyObject({ a: 1 }); // false
* isEmptyObject([]); // false
* isEmptyObject(null); // false
*/
function isEmptyObject(value) {
	return isPlainObject(value) && Object.keys(value).length === 0;
}
//#endregion
export { isEmptyObject };
