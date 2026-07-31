const require_toNumber = require("./toNumber.js");
//#region src/compat/util/lt.ts
/**
* Checks if value is less than other.
*
* @param value The value to compare.
* @param other The other value to compare.
* @returns Returns `true` if value is less than other, else `false`.
*
* @example
* lt(1, 3); // true
* lt(3, 3); // false
* lt(3, 1); // false
*/
function lt(value, other) {
	if (typeof value === "string" && typeof other === "string") return value < other;
	return require_toNumber.toNumber(value) < require_toNumber.toNumber(other);
}
//#endregion
exports.lt = lt;
