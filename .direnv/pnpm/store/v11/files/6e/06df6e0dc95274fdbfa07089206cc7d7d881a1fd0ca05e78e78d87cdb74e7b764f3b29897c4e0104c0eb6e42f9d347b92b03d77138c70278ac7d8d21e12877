//#region src/array/takeRight.ts
/**
* Returns a new array containing the last `count` elements from the input array `arr`.
* If `count` is greater than the length of `arr`, the entire array is returned.
*
* @template T - The type of elements in the array.
* @param arr - The array to take elements from.
* @param count - The number of elements to take.
* @returns A new array containing the last `count` elements from `arr`.
*
* @example
* // Returns [4, 5]
* takeRight([1, 2, 3, 4, 5], 2);
*
* @example
* // Returns ['b', 'c']
* takeRight(['a', 'b', 'c'], 2);
*
* @example
* // Returns [1, 2, 3]
* takeRight([1, 2, 3], 5);
*/
function takeRight(arr, count) {
	if (count <= 0 || arr.length === 0) return [];
	return arr.slice(-count);
}
//#endregion
export { takeRight };
