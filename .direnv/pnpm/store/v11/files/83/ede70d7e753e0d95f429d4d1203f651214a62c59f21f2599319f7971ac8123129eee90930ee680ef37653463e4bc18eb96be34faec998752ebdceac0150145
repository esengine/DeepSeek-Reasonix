//#region src/set/filter.ts
/**
* Filters a Set based on a predicate function.
*
* This function takes a Set and a predicate function, and returns a new Set containing
* only the elements for which the predicate function returns true.
*
* @template T - The type of elements in the Set.
* @param set - The Set to filter.
* @param callback - A predicate function that tests each element.
* @returns A new Set containing only the elements that satisfy the predicate.
*
* @example
* const set = new Set([1, 2, 3, 4, 5]);
* const result = filter(set, (value) => value > 2);
* // result will be:
* // Set(3) { 3, 4, 5 }
*/
function filter(set, callback) {
	const result = /* @__PURE__ */ new Set();
	for (const value of set) if (callback(value, value, set)) result.add(value);
	return result;
}
//#endregion
export { filter };
