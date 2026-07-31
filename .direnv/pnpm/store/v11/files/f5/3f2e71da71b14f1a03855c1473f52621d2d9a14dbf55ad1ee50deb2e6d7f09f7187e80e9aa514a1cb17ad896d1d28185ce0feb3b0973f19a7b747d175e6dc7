//#region src/array/zipWith.ts
/**
* Combines multiple arrays into a single array using a custom combiner function.
*
* This function takes one array and a variable number of additional arrays,
* applying the provided combiner function to the corresponding elements of each array.
* If the input arrays are of different lengths, the resulting array will have the length
* of the longest input array, with undefined values for missing elements.
*
* @template T - The type of elements in the input arrays.
* @template R - The type of elements in the resulting array.
* @param arr1 - The first array to zip.
* @param rest - The additional arrays to zip together, followed by the combiner function.
* @param combine - The combiner function that takes corresponding elements from each array, followed by their index, and returns a single value.
* @returns A new array where each element is the result of applying the combiner function to the corresponding elements of the input arrays.
*
* @example
* const arr1 = [1, 2, 3];
* const arr2 = ['a', 'b', 'c'];
* const result = zipWith(arr1, arr2, (num, char) => `${num}${char}`);
* // result will be ['1a', '2b', '3c']
*/
function zipWith(arr1, ...rest) {
	const arrs = [arr1, ...rest.slice(0, -1)];
	const combine = rest[rest.length - 1];
	const maxIndex = Math.max(...arrs.map((arr) => arr.length));
	const result = Array(maxIndex);
	for (let i = 0; i < maxIndex; i++) result[i] = combine(...arrs.map((arr) => arr[i]), i);
	return result;
}
//#endregion
exports.zipWith = zipWith;
