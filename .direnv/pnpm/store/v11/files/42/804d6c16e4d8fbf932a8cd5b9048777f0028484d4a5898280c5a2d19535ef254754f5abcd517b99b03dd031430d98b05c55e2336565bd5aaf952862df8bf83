//#region src/array/takeWhile.ts
/**
* Returns a new array containing the leading elements of the provided array
* that satisfy the provided predicate function. It stops taking elements as soon
* as an element does not satisfy the predicate.
*
* @template T - The type of elements in the array.
* @param arr - The array to process.
* @param shouldContinueTaking - The predicate function that is called with each element, its index, and the array. Elements are included in the result as long as this function returns true.
* @returns A new array containing the leading elements that satisfy the predicate.
*
* @example
* // Returns [1, 2]
* takeWhile([1, 2, 3, 4], x => x < 3);
*
* @example
* // Returns []
* takeWhile([1, 2, 3, 4], x => x > 3);
*
* @example
* // Using index parameter
* takeWhile([10, 20, 30, 40], (x, index) => index < 2);
* // Returns: [10, 20]
*/
function takeWhile(arr, shouldContinueTaking) {
	const result = [];
	for (let i = 0; i < arr.length; i++) {
		const item = arr[i];
		if (!shouldContinueTaking(item, i, arr)) break;
		result.push(item);
	}
	return result;
}
//#endregion
export { takeWhile };
