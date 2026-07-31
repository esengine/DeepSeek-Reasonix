const require_toNumber = require("./toNumber.js");
//#region src/compat/util/gt.ts
/**
* Checks if value is greater than other.
*
* @param value The value to compare.
* @param other The other value to compare.
* @returns Returns `true` if value is greater than other, else `false`.
*
* @example
* gt(3, 1); // true
* gt(3, 3); // false
* gt(1, 3); // false
*/
function gt(value, other) {
	if (typeof value === "string" && typeof other === "string") return value > other;
	return require_toNumber.toNumber(value) > require_toNumber.toNumber(other);
}
//#endregion
exports.gt = gt;
