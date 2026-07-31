//#region src/array/fill.ts
/**
* Fills elements of an array with a specified value from the start position up to, but not including, the end position.
*
* This function mutates the original array and replaces its elements with the provided value, starting from the specified
* start index up to the end index (non-inclusive). If the start or end indices are not provided, it defaults to filling the
* entire array.
*
* @template T - The type of elements in the original array.
* @template U - The type of the value to fill the array with.
* @param array - The array to fill.
* @param value - The value to fill the array with.
* @param [start=0] - The start position. Defaults to 0.
* @param [end=arr.length] - The end position. Defaults to the array's length.
* @returns The array with the filled values.
*
* @example
* fill([1, 2, 3], 'a');
* // => ['a', 'a', 'a']
*
* fill(Array(3), 2);
* // => [2, 2, 2]
*
* fill([4, 6, 8, 10], '*', 1, 3);
* // => [4, '*', '*', 10]
*
* fill([1, 2, 3], '*', -2, -1);
* // => [1, '*', 3]
*/
function fill(array, value, start = 0, end = array.length) {
	const length = array.length;
	const finalStart = Math.max(start >= 0 ? start : length + start, 0);
	const finalEnd = Math.min(end >= 0 ? end : length + end, length);
	for (let i = finalStart; i < finalEnd; i++) array[i] = value;
	return array;
}
//#endregion
export { fill };
