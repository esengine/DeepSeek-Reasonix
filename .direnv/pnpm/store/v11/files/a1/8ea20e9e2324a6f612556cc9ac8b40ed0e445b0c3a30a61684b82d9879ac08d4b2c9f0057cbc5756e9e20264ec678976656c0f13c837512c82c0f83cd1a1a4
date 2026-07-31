import { identity } from "../../function/identity.mjs";
import { isArrayLike } from "../predicate/isArrayLike.mjs";
import { iteratee } from "../util/iteratee.mjs";
//#region src/compat/array/filter.ts
/**
* Iterates over the collection and filters elements based on the given predicate.
* If a function is provided, it is invoked for each element in the collection.
*
* @template T
* @param source - The array or object to iterate over.
* @param [predicate=identity] - The function invoked per iteration.
* @returns Returns a new array of filtered elements that satisfy the predicate.
*
* @example
* filter([{ a: 1 }, { a: 2 }, { b: 1 }], 'a');
* // => [{ a: 1 }, { a: 2 }]
*
* filter([{ a: 1 }, { a: 2 }, { b: 1 }], { b: 1 });
* // => [{ b: 1 }]
*
* filter({ item1: { a: 0, b: true }, item2: { a: 1, b: true }, item3: { a: 2, b: false }}, { b: false })
* // => [{ a: 2, b: false }]
*
* filter([{ a: 1 }, { a: 2 }, { a: 3 }], ['a', 2]);
* // => [{ a: 2 }]
*/
function filter(source, predicate = identity) {
	if (!source) return [];
	predicate = iteratee(predicate);
	if (!Array.isArray(source)) {
		const result = [];
		const keys = Object.keys(source);
		const length = isArrayLike(source) ? source.length : keys.length;
		for (let i = 0; i < length; i++) {
			const key = keys[i];
			const value = source[key];
			if (predicate(value, key, source)) result.push(value);
		}
		return result;
	}
	const result = [];
	const length = source.length;
	for (let i = 0; i < length; i++) {
		const value = source[i];
		if (predicate(value, i, source)) result.push(value);
	}
	return result;
}
//#endregion
export { filter };
