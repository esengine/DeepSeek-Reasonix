const require_uniqBy = require("./uniqBy.js");
//#region src/array/unionBy.ts
/**
* Creates an array of unique values, in order, from all given arrays using a provided mapping function to determine equality.
*
* @template T - The type of elements in the array.
* @template U - The type of mapped elements.
* @param arr1 - The first array.
* @param arr2 - The second array.
* @param mapper - The function to map array elements to comparison values.
* @returns A new array containing the union of unique elements from `arr1` and `arr2`, based on the values returned by the mapping function.
*
* @example
* // Custom mapping function for numbers (modulo comparison)
* const moduloMapper = (x) => x % 3;
* unionBy([1, 2, 3], [4, 5, 6], moduloMapper);
* // Returns [1, 2, 3]
*
* @example
* // Custom mapping function for objects with an 'id' property
* const idMapper = (obj) => obj.id;
* unionBy([{ id: 1 }, { id: 2 }], [{ id: 2 }, { id: 3 }], idMapper);
* // Returns [{ id: 1 }, { id: 2 }, { id: 3 }]
*/
function unionBy(arr1, arr2, mapper) {
	return require_uniqBy.uniqBy(arr1.concat(arr2), mapper);
}
//#endregion
exports.unionBy = unionBy;
