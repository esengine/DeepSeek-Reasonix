import { identity } from "../../function/identity.mjs";
import { isFunction } from "../../predicate/isFunction.mjs";
import { isObject } from "../predicate/isObject.mjs";
import { iteratee } from "../util/iteratee.mjs";
import { forEach } from "../array/forEach.mjs";
import { isTypedArray } from "../predicate/isTypedArray.mjs";
import { isBuffer } from "../predicate/isBuffer.mjs";
//#region src/compat/object/transform.ts
/**
* Traverses object values and creates a new object by accumulating them in the desired form.
*
* If no initial value is provided for `accumulator`, it creates a new array or object with the same prototype.
*
The traversal is interrupted when the `iteratee` function returns `false`.
*
* @template T - The type of object.
* @template U - The type of accumulator.
* @param object - The object to iterate over.
* @param [iteratee] - The function invoked per iteration.
* @param [accumulator] - The initial value.
* @returns Returns the accumulated value.
*
* @example
* // Transform an array
* const array = [2, 3, 4];
* transform(array, (acc, value) => { acc += value; return value % 2 === 0; }, 0) // => 5
*
* @example
* // Transform an object
* const obj = { 'a': 1, 'b': 2, 'c': 1 };
* transform(obj, (result, value, key) => { (result[value] || (result[value] = [])).push(key) }, {}) // => { '1': ['a', 'c'], '2': ['b'] }
*/
function transform(object, iteratee$1 = identity, accumulator) {
	const isArrayOrBufferOrTypedArray = Array.isArray(object) || isBuffer(object) || isTypedArray(object);
	iteratee$1 = iteratee(iteratee$1);
	if (accumulator == null) if (isArrayOrBufferOrTypedArray) accumulator = [];
	else if (isObject(object) && isFunction(object.constructor)) accumulator = Object.create(Object.getPrototypeOf(object));
	else accumulator = {};
	if (object == null) return accumulator;
	forEach(object, (value, key, object) => iteratee$1(accumulator, value, key, object));
	return accumulator;
}
//#endregion
export { transform };
