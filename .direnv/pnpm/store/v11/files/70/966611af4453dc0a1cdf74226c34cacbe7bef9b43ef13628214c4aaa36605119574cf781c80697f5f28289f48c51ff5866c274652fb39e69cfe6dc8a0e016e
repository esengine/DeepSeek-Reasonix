//#region src/_internal/isEqualsSameValueZero.ts
/**
* Performs a `SameValueZero` comparison between two values to determine if they are equivalent.
*
* @param {any} value - The value to compare.
* @param {any} other - The other value to compare.
* @returns {boolean} Returns `true` if the values are equivalent, else `false`.
*
* @example
* eq(1, 1); // true
* eq(0, -0); // true
* eq(NaN, NaN); // true
* eq('a', Object('a')); // false
*/
function isEqualsSameValueZero(value, other) {
	return value === other || Number.isNaN(value) && Number.isNaN(other);
}
//#endregion
exports.isEqualsSameValueZero = isEqualsSameValueZero;
