//#region src/array/flatten.ts
/**
* Flattens an array up to the specified depth.
*
* @template T - The type of elements within the array.
* @template D - The depth to which the array should be flattened.
* @param arr - The array to flatten.
* @param depth - The depth level specifying how deep a nested array structure should be flattened. Defaults to 1.
* @returns A new array that has been flattened.
*
* @example
* const arr = flatten([1, [2, 3], [4, [5, 6]]], 1);
* // Returns: [1, 2, 3, 4, [5, 6]]
*
* const arr = flatten([1, [2, 3], [4, [5, 6]]], 2);
* // Returns: [1, 2, 3, 4, 5, 6]
*/
function flatten(arr, depth = 1) {
	const result = [];
	const flooredDepth = Math.floor(depth);
	const recursive = (arr, currentDepth) => {
		for (let i = 0; i < arr.length; i++) {
			const item = arr[i];
			if (Array.isArray(item) && currentDepth < flooredDepth) recursive(item, currentDepth + 1);
			else result.push(item);
		}
	};
	recursive(arr, 0);
	return result;
}
//#endregion
export { flatten };
