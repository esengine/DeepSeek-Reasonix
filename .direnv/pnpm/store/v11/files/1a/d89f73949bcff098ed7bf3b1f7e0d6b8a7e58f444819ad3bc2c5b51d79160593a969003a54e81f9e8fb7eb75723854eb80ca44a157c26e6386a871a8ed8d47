//#region src/array/remove.ts
/**
* Removes elements from an array based on a predicate function.
*
* This function changes `arr` in place.
* If you want to remove elements without modifying the original array, use `filter`.
*
* @template T
* @param arr - The array to modify.
* @param shouldRemoveElement - The function invoked per iteration to determine if an element should be removed.
* @returns The modified array with the specified elements removed.
*
* @example
* const numbers = [1, 2, 3, 4, 5];
* remove(numbers, (value) => value % 2 === 0);
* console.log(numbers); // [1, 3, 5]
*/
function remove(arr, shouldRemoveElement) {
	const originalArr = arr.slice();
	const removed = [];
	let resultIndex = 0;
	for (let i = 0; i < arr.length; i++) {
		if (shouldRemoveElement(arr[i], i, originalArr)) {
			removed.push(arr[i]);
			continue;
		}
		if (!Object.hasOwn(arr, i)) {
			delete arr[resultIndex++];
			continue;
		}
		arr[resultIndex++] = arr[i];
	}
	arr.length = resultIndex;
	return removed;
}
//#endregion
export { remove };
