const require_identity = require("../../function/identity.js");
const require_meanBy = require("../../math/meanBy.js");
const require_iteratee = require("../util/iteratee.js");
//#region src/compat/math/meanBy.ts
/**
* Calculates the average of an array of numbers when applying
* the `iteratee` function to each element.
*
* If the array is empty, this function returns `NaN`.
*
* @template T - The type of elements in the array.
* @param items An array to calculate the average.
* @param iteratee
* The criteria used to determine the maximum value.
*  - If a **function** is provided, it extracts a numeric value from each element.
*  - If a **string** is provided, it is treated as a key to extract values from the objects.
*  - If a **[key, value]** pair is provided, it matches elements with the specified key-value pair.
*  - If an **object** is provided, it matches elements that contain the specified properties.
* @returns The average of all the numbers as determined by the `iteratee` function.
*
* @example
* meanBy([{ a: 1 }, { a: 2 }, { a: 3 }], x => x.a); // Returns: 2
* meanBy([], x => x.a); // Returns: NaN
* meanBy([[2], [3], [1]], 0); // Returns: 2
* meanBy([{ a: 2 }, { a: 3 }, { a: 1 }], 'a'); // Returns: 2
*/
function meanBy(items, iteratee$1) {
	if (items == null) return NaN;
	return require_meanBy.meanBy(Array.from(items), require_iteratee.iteratee(iteratee$1 ?? require_identity.identity));
}
//#endregion
exports.meanBy = meanBy;
