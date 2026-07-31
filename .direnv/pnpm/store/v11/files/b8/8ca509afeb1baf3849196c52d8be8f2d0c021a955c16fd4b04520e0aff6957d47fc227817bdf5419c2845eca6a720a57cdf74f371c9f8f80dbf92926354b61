import { identity } from "../../function/identity.mjs";
import { iteratee } from "../util/iteratee.mjs";
import { filter } from "./filter.mjs";
import { negate } from "../function/negate.mjs";
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
function reject(source, predicate = identity) {
	return filter(source, negate(iteratee(predicate)));
}
//#endregion
export { reject };
