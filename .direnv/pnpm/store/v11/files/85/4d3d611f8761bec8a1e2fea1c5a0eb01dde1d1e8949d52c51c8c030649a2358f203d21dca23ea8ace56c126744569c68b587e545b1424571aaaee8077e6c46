//#region src/array/pull.ts
/**
* Removes all specified values from an array.
*
* This function changes `arr` in place.
* If you want to remove values without modifying the original array, use `difference`.
*
* @template T
* @param arr - The array to modify.
* @param valuesToRemove - The values to remove from the array.
* @returns The modified array with the specified values removed.
*
* @example
* const numbers = [1, 2, 3, 4, 5, 2, 4];
* pull(numbers, [2, 4]);
* console.log(numbers); // [1, 3, 5]
*/
function pull(arr, valuesToRemove) {
	const valuesSet = new Set(valuesToRemove);
	let resultIndex = 0;
	for (let i = 0; i < arr.length; i++) {
		if (valuesSet.has(arr[i])) continue;
		if (!Object.hasOwn(arr, i)) {
			delete arr[resultIndex++];
			continue;
		}
		arr[resultIndex++] = arr[i];
	}
	arr.length = resultIndex;
	return arr;
}
//#endregion
export { pull };
