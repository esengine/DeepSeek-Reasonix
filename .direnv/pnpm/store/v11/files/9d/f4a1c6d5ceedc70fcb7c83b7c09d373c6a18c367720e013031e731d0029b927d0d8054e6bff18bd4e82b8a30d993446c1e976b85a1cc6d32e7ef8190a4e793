import { toNumber } from "./toNumber.mjs";
//#region src/compat/util/lte.ts
/**
* Checks if value is less than or equal to other.
*
* @param value The value to compare.
* @param other The other value to compare.
* @returns Returns `true` if value is less than or equal to other, else `false`.
*
* @example
* lte(1, 3); // => true
* lte(3, 3); // => true
* lte(3, 1); // => false
*/
function lte(value, other) {
	if (typeof value === "string" && typeof other === "string") return value <= other;
	return toNumber(value) <= toNumber(other);
}
//#endregion
export { lte };
