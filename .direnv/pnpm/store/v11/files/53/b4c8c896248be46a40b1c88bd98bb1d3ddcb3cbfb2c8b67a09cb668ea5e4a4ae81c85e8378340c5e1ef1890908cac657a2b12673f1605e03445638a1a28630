const require_identity = require("../../function/identity.js");
const require_isFunction = require("../../predicate/isFunction.js");
const require_isObject = require("../predicate/isObject.js");
const require_iteratee = require("../util/iteratee.js");
const require_forEach = require("../array/forEach.js");
const require_isTypedArray = require("../predicate/isTypedArray.js");
const require_isBuffer = require("../predicate/isBuffer.js");
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
function transform(object, iteratee$1 = require_identity.identity, accumulator) {
	const isArrayOrBufferOrTypedArray = Array.isArray(object) || require_isBuffer.isBuffer(object) || require_isTypedArray.isTypedArray(object);
	iteratee$1 = require_iteratee.iteratee(iteratee$1);
	if (accumulator == null) if (isArrayOrBufferOrTypedArray) accumulator = [];
	else if (require_isObject.isObject(object) && require_isFunction.isFunction(object.constructor)) accumulator = Object.create(Object.getPrototypeOf(object));
	else accumulator = {};
	if (object == null) return accumulator;
	require_forEach.forEach(object, (value, key, object) => iteratee$1(accumulator, value, key, object));
	return accumulator;
}
//#endregion
exports.transform = transform;
