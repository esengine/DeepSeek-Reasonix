const require_isEqualsSameValueZero = require("../_internal/isEqualsSameValueZero.js");
//#region src/map/hasValue.ts
/**
* Checks if a Map contains a specific value.
*
* This function iterates through all values in the Map and checks if any value
* is equal to the search element using SameValueZero comparison (similar to
* Array.prototype.includes). This means that NaN is considered equal to NaN.
*
* @template K - The type of keys in the Map.
* @template V - The type of values in the Map.
* @param map - The Map to search.
* @param searchElement - The value to search for.
* @returns true if the Map contains the value, false otherwise.
*
* @example
* const map = new Map([
*   ['a', 1],
*   ['b', 2],
*   ['c', 3]
* ]);
* const result = hasValue(map, 2);
* // result will be: true
*
* const result2 = hasValue(map, 5);
* // result2 will be: false
*/
function hasValue(map, searchElement) {
	for (const value of map.values()) if (require_isEqualsSameValueZero.isEqualsSameValueZero(value, searchElement)) return true;
	return false;
}
//#endregion
exports.hasValue = hasValue;
