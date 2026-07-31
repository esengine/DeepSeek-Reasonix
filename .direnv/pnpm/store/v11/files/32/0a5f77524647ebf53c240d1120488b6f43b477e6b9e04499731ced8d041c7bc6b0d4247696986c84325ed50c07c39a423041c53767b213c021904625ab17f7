const require_sumBy = require("./sumBy.js");
//#region src/math/meanBy.ts
/**
* Calculates the average of an array of numbers when applying
* the `getValue` function to each element.
*
* If the array is empty, this function returns `NaN`.
*
* @template T - The type of elements in the array.
* @param items An array to calculate the average.
* @param getValue A function that selects a numeric value from each element.
* @returns The average of all the numbers as determined by the `getValue` function.
*
* @example
* meanBy([{ a: 1 }, { a: 2 }, { a: 3 }], x => x.a); // Returns: 2
* meanBy([], x => x.a); // Returns: NaN
*/
function meanBy(items, getValue) {
	return require_sumBy.sumBy(items, (item) => getValue(item)) / items.length;
}
//#endregion
exports.meanBy = meanBy;
