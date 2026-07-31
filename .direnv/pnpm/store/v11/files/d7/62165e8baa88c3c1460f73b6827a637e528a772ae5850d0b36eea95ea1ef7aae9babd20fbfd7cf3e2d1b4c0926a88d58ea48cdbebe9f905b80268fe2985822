const require_identity = require("../../function/identity.js");
const require_iteratee = require("../util/iteratee.js");
const require_filter = require("./filter.js");
const require_negate = require("../function/negate.js");
//#region src/compat/array/reject.ts
/**
* Iterates over the collection and rejects elements based on the given predicate.
* If a function is provided, it is invoked for each element in the collection.
*
* @template T
* @param source - The array or object to iterate over.
* @param [predicate=identity] - The function invoked per iteration.
* @returns Returns a new array of elements that do not satisfy the predicate.
*
* @example
* reject([{ a: 1 }, { a: 2 }, { b: 1 }], 'a');
* // => [{ b: 1 }]
*
* reject([{ a: 1 }, { a: 2 }, { b: 1 }], { b: 1 });
* // => [{ a: 1 }, { a: 2 }]
*
* reject({ item1: { a: 0, b: true }, item2: { a: 1, b: true }, item3: { a: 2, b: false }}, { b: false })
* // => [{ a: 0, b: true }, { a: 1, b: true }]
*
* reject([{ a: 1 }, { a: 2 }, { a: 3 }], ['a', 2]);
* // => [{ a: 1 }, { a: 3 }]
*/
function reject(source, predicate = require_identity.identity) {
	return require_filter.filter(source, require_negate.negate(require_iteratee.iteratee(predicate)));
}
//#endregion
exports.reject = reject;
