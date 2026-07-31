//#region src/array/chunkBy.ts
/**
* Splits an array into chunks of consecutive elements that share the same key.
*
* Walking left to right, each element's key is derived by `iteratee`. Whenever
* the key differs from the previous element's key, a new chunk is started;
* otherwise the element is appended to the current chunk. Keys are compared with
* `!==` (strict inequality), so equal primitives stay together while distinct
* object references always start a new chunk.
*
* Unlike {@link chunk}, which splits by a fixed size, `chunkBy` splits by a
* boundary condition, keeping runs of same-keyed elements together.
*
* @template T - The type of elements in the array.
* @param arr - The array to split into chunks.
* @param iteratee - A function that derives the comparison key for each element.
* @returns A two-dimensional array where each sub-array is a run of consecutive
*   elements that produced the same key.
*
* @example
* // Group consecutive equal numbers
* chunkBy([1, 1, 2, 3, 3, 3], value => value);
* // Returns: [[1, 1], [2], [3, 3, 3]]
*
* @example
* // Group consecutive words by their length
* chunkBy(['a', 'b', 'cd', 'ef', 'g'], word => word.length);
* // Returns: [['a', 'b'], ['cd', 'ef'], ['g']]
*/
function chunkBy(arr, iteratee) {
	const result = [];
	let prevKey;
	for (let i = 0; i < arr.length; i++) {
		const key = iteratee(arr[i]);
		if (i === 0 || key !== prevKey) result.push([arr[i]]);
		else result[result.length - 1].push(arr[i]);
		prevKey = key;
	}
	return result;
}
//#endregion
exports.chunkBy = chunkBy;
