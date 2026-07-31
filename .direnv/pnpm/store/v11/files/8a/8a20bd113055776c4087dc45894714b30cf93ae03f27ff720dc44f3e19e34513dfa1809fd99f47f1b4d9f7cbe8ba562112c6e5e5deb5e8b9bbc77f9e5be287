//#region src/compat/array/castArray.ts
/**
* Casts value as an array if it's not one.
*
* @template T The type of elements in the array.
* @param value The value to be cast to an array.
* @returns An array containing the input value if it wasn't an array, or the original array if it was.
*
* @example
* const arr1 = castArray(1);
* // Returns: [1]
*
* const arr2 = castArray([1]);
* // Returns: [1]
*
* const arr3 = castArray({'a': 1});
* // Returns: [{'a': 1}]
*
* const arr4 = castArray(null);
* // Returns: [null]
*
* const arr5 = castArray(undefined);
* // Returns: [undefined]
*
* const arr6 = castArray();
* // Returns: []
*/
function castArray(value) {
	if (arguments.length === 0) return [];
	return Array.isArray(value) ? value : [value];
}
//#endregion
exports.castArray = castArray;
