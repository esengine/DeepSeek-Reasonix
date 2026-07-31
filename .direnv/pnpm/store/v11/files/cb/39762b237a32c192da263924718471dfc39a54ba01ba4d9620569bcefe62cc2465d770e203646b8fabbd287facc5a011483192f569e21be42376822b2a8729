const require_identity = require("../../function/identity.js");
const require_isArrayLike = require("../predicate/isArrayLike.js");
const require_iteratee = require("../util/iteratee.js");
//#region src/compat/array/partition.ts
/**
* Creates an array of elements split into two groups, the first of which contains elements
* `predicate` returns truthy for, the second of which contains elements
* `predicate` returns falsy for. The predicate is invoked with one argument: (value).
*
* @template T
* @param source - The array or object to iterate over.
* @param [predicate=identity] - The function invoked per iteration.
* @returns Returns the array of grouped elements.
*
* @example
* partition([{ a: 1 }, { a: 2 }, { b: 1 }], 'a');
* // => [[{ a: 1 }, { a: 2 }], [{ b: 1 }]]
*
* partition([{ a: 1 }, { a: 2 }, { b: 1 }], { b: 1 });
* // => [[{ b: 1 }], [{ a: 1 }, { a: 2 }]]
*
* partition({ item1: { a: 0, b: true }, item2: { a: 1, b: true }, item3: { a: 2, b: false }}, { b: false })
* // => [[{ a: 2, b: false }], [{ a: 0, b: true }, { a: 1, b: true }]]
*
* partition([{ a: 1 }, { a: 2 }, { a: 3 }], ['a', 2]);
* // => [[{ a: 2 }], [{ a: 1 }, { a: 3 }]]
*/
function partition(source, predicate = require_identity.identity) {
	if (!source) return [[], []];
	const collection = require_isArrayLike.isArrayLike(source) ? source : Object.values(source);
	predicate = require_iteratee.iteratee(predicate);
	const matched = [];
	const unmatched = [];
	for (let i = 0; i < collection.length; i++) {
		const value = collection[i];
		if (predicate(value)) matched.push(value);
		else unmatched.push(value);
	}
	return [matched, unmatched];
}
//#endregion
exports.partition = partition;
