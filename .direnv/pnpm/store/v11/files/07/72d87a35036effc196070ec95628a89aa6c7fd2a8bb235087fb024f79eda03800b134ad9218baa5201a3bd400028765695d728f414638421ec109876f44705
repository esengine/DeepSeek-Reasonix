//#region src/array/shuffle.ts
/**
* Randomizes the order of elements in an array using the Fisher-Yates algorithm.
*
* This function takes an array and returns a new array with its elements shuffled in a random order.
*
* @template T - The type of elements in the array.
* @param arr - The array to shuffle.
* @returns A new array with its elements shuffled in random order.
*
* @example
* const array = [1, 2, 3, 4, 5];
* const shuffledArray = shuffle(array);
* // shuffledArray will be a new array with elements of array in random order, e.g., [3, 1, 4, 5, 2]
*/
function shuffle(arr) {
	const result = arr.slice();
	/**
	* https://en.wikipedia.org/wiki/Fisher%E2%80%93Yates_shuffle#The_modern_algorithm
	*/
	for (let i = result.length - 1; i >= 1; i--) {
		const j = Math.floor(Math.random() * (i + 1));
		[result[i], result[j]] = [result[j], result[i]];
	}
	return result;
}
//#endregion
export { shuffle };
