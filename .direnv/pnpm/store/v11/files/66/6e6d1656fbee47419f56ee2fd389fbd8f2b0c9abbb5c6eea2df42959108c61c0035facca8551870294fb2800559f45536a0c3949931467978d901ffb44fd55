const require_differenceWith = require("./differenceWith.js");
//#region src/array/isSubsetWith.ts
/**
* Checks if the `subset` array is entirely contained within the `superset` array based on a custom equality function.
*
* This function takes two arrays and a custom comparison function. It returns a boolean indicating
* whether all elements in the subset array are present in the superset array, as determined by the provided
* custom equality function.
*
* @template T - The type of elements contained in the arrays.
* @param superset - The array that may contain all elements of the subset.
* @param subset - The array to check against the superset.
* @param areItemsEqual - A function to determine if two items are equal.
* @returns Returns `true` if all elements of the subset are present in the superset
* according to the custom equality function, otherwise returns `false`.
*
* @example
* ```typescript
* const superset = [{ id: 1 }, { id: 2 }, { id: 3 }];
* const subset = [{ id: 2 }, { id: 1 }];
* const areItemsEqual = (a, b) => a.id === b.id;
* isSubsetWith(superset, subset, areItemsEqual); // true
* ```
*
* @example
* ```typescript
* const superset = [{ id: 1 }, { id: 2 }, { id: 3 }];
* const subset = [{ id: 4 }];
* const areItemsEqual = (a, b) => a.id === b.id;
* isSubsetWith(superset, subset, areItemsEqual); // false
* ```
*/
function isSubsetWith(superset, subset, areItemsEqual) {
	return require_differenceWith.differenceWith(subset, superset, areItemsEqual).length === 0;
}
//#endregion
exports.isSubsetWith = isSubsetWith;
