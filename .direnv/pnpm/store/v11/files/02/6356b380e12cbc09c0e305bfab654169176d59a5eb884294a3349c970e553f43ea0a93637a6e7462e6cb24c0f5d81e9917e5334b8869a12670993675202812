//#region src/array/forEachRight.ts
/**
* Iterates over elements of 'arr' from right to left and invokes 'callback' for each element.
*
* @template T - The type of elements in the array.
* @param arr - The array to iterate over.
* @param callback - The function invoked per iteration.
* The callback function receives three arguments:
*  - 'value': The current element being processed in the array.
*  - 'index': The index of the current element being processed in the array.
*  - 'arr': The array 'forEachRight' was called upon.
*
* @example
* const array = [1, 2, 3];
* const result: number[] = [];
*
* // Use the forEachRight function to iterate through the array and add each element to the result array.
* forEachRight(array, (value) => {
*  result.push(value);
* })
*
* console.log(result) // Output: [3, 2, 1]
*/
function forEachRight(arr, callback) {
	for (let i = arr.length - 1; i >= 0; i--) {
		const element = arr[i];
		callback(element, i, arr);
	}
}
//#endregion
export { forEachRight };
