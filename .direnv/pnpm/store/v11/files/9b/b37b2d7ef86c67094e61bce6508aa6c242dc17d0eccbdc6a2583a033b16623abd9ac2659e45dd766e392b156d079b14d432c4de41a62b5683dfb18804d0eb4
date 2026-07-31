//#region src/array/combinations.ts
/**
* Returns all `r`-length combinations of elements from the input array.
*
* Combinations are emitted in lexicographic order based on the position of elements in the input array.
* Elements are treated as unique by position, not by value, so duplicates in the input may produce
* combinations that look identical.
*
* The number of combinations is `n! / r! / (n - r)!` when `0 <= r <= n`, and zero when `r > n`.
*
* @template T
* @param arr - The input array.
* @param r - The length of each combination. Must be a non-negative integer.
* @returns An array of `r`-length combinations.
* @throws {Error} If `r` is not a non-negative integer.
*
* @example
* combinations(['A', 'B', 'C', 'D'], 2);
* // => [['A','B'], ['A','C'], ['A','D'], ['B','C'], ['B','D'], ['C','D']]
*
* @example
* combinations([1, 2, 3, 4], 3);
* // => [[1,2,3], [1,2,4], [1,3,4], [2,3,4]]
*
* @example
* combinations([1, 2, 3], 0);
* // => [[]]
*
* @example
* combinations([1, 2], 5);
* // => []
*/
function combinations(arr, r) {
	if (!Number.isInteger(r) || r < 0) throw new Error("r must be a non-negative integer.");
	const n = arr.length;
	if (r > n) return [];
	if (r === 0) return [[]];
	const indices = Array(r);
	for (let i = 0; i < r; i++) indices[i] = i;
	const result = [];
	while (true) {
		const tuple = Array(r);
		for (let i = 0; i < r; i++) tuple[i] = arr[indices[i]];
		result.push(tuple);
		let i = r - 1;
		while (i >= 0 && indices[i] === i + n - r) i--;
		if (i < 0) return result;
		indices[i]++;
		for (let j = i + 1; j < r; j++) indices[j] = indices[j - 1] + 1;
	}
}
//#endregion
exports.combinations = combinations;
