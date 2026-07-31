import { intersectionWith as intersectionWith$1 } from "../../array/intersectionWith.mjs";
import { isEqualsSameValueZero } from "../../_internal/isEqualsSameValueZero.mjs";
import "../util/eq.mjs";
import { last } from "./last.mjs";
import { uniq } from "./uniq.mjs";
//#region src/compat/array/intersectionWith.ts
/**
* Returns the intersection of multiple arrays based on a custom equality function.
*
* @template T - The type of elements in the arrays
* @param firstArr - The first array to compare
* @param otherArrs - Additional arrays and optional equality function
* @returns Elements from first array that match in all arrays
*
* @example
* const arr1 = [{id: 1}, {id: 2}];
* const arr2 = [{id: 2}, {id: 3}];
* const result = intersectionWith(arr1, arr2, (a, b) => a.id === b.id);
* // result is [{id: 2}]
*/
function intersectionWith(firstArr, ...otherArrs) {
	if (firstArr == null) return [];
	const _comparator = last(otherArrs);
	let comparator = isEqualsSameValueZero;
	let uniq$1 = uniq;
	if (typeof _comparator === "function") {
		comparator = _comparator;
		uniq$1 = uniqPreserve0;
		otherArrs.pop();
	}
	let result = uniq$1(Array.from(firstArr));
	for (let i = 0; i < otherArrs.length; ++i) {
		const otherArr = otherArrs[i];
		if (otherArr == null) return [];
		result = intersectionWith$1(result, Array.from(otherArr), comparator);
	}
	return result;
}
/**
* This function is to preserve the sign of `-0`, which is a behavior in lodash.
*/
function uniqPreserve0(arr) {
	const result = [];
	const added = /* @__PURE__ */ new Set();
	for (let i = 0; i < arr.length; i++) {
		const item = arr[i];
		if (added.has(item)) continue;
		result.push(item);
		added.add(item);
	}
	return result;
}
//#endregion
export { intersectionWith };
