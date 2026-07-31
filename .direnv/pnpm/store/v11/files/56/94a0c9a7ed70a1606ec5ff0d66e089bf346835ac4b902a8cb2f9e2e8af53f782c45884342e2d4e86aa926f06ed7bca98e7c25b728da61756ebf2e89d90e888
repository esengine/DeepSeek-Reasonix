const require_sumBy = require("./sumBy.js");
//#region src/compat/math/sum.ts
/**
* Computes the sum of the values that are returned by the `iteratee` function.
*
* It does not coerce values to `number`.
*
* @param array - The array to iterate over.
* @returns Returns the sum.
*
* @example
* sum([1, 2, 3]); // => 6
* sum([1n, 2n, 3n]); // => 6n
* sum(["1", "2"]); // => "12"
* sum([1, undefined, 2]); // => 3
* sum(null); // => 0
* sum(undefined); // => 0
*/
function sum(array) {
	return require_sumBy.sumBy(array);
}
//#endregion
exports.sum = sum;
