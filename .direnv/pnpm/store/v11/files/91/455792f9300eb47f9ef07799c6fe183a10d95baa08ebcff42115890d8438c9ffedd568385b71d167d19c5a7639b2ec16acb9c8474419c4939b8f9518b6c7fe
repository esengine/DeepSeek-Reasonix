import { sortedIndexBy } from "./sortedIndexBy.mjs";
//#region src/compat/array/sortedLastIndexBy.ts
/**
* This method is like `sortedLastIndex` except that it accepts `iteratee`
* which is invoked for `value` and each element of `array` to compute their
* sort ranking. The iteratee is invoked with one argument: (value).
*
* @param array The sorted array to inspect.
* @param value The value to evaluate.
* @param iteratee The iteratee invoked per element.
* @returns Returns the highest index at which `value` should be inserted
*  into `array`.
* @example
* const objects = [{ 'n': 4 }, { 'n': 5 }]
* sortedLastIndexBy(objects, { 'n': 4 }, ({ n }) => n)
* // => 1
*/
function sortedLastIndexBy(array, value, iteratee) {
	return sortedIndexBy(array, value, iteratee, true);
}
//#endregion
export { sortedLastIndexBy };
