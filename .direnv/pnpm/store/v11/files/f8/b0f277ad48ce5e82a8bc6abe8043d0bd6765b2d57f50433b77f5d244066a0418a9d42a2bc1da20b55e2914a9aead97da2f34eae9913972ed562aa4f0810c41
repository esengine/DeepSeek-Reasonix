import { dropWhile as dropWhile$1 } from "../../array/dropWhile.mjs";
import { identity } from "../../function/identity.mjs";
import { toArray } from "../_internal/toArray.mjs";
import { isArrayLike } from "../predicate/isArrayLike.mjs";
import { property } from "../object/property.mjs";
import { matches } from "../predicate/matches.mjs";
import { matchesProperty } from "../predicate/matchesProperty.mjs";
//#region src/compat/array/dropWhile.ts
/**
* Creates a slice of array excluding elements dropped from the beginning.
* Elements are dropped until predicate returns falsey.
* The predicate is invoked with three arguments: (value, index, array).
*
* @template T - The type of elements in the array
* @param array - The array to query
* @param [predicate=identity] - The function invoked per iteration
* @returns Returns the slice of array
*
* @example
* dropWhile([1, 2, 3], n => n < 3)
* // => [3]
*
* dropWhile([{ a: 1, b: 2 }, { a: 1, b: 3 }], { a: 1 })
* // => []
*
* dropWhile([{ a: 1, b: 2 }, { a: 1, b: 3 }], ['a', 1])
* // => []
*
* dropWhile([{ a: 1, b: 2 }, { a: 1, b: 3 }], 'a')
* // => []
*/
function dropWhile(array, predicate = identity) {
	if (!isArrayLike(array)) return [];
	return dropWhileImpl(toArray(array), predicate);
}
function dropWhileImpl(arr, predicate) {
	switch (typeof predicate) {
		case "function": return dropWhile$1(arr, (item, index, arr) => Boolean(predicate(item, index, arr)));
		case "object": if (Array.isArray(predicate) && predicate.length === 2) {
			const key = predicate[0];
			const value = predicate[1];
			return dropWhile$1(arr, matchesProperty(key, value));
		} else return dropWhile$1(arr, matches(predicate));
		case "number":
		case "symbol":
		case "string": return dropWhile$1(arr, property(predicate));
	}
}
//#endregion
export { dropWhile };
