//#region src/predicate/isIterable.ts
/**
* Checks if a value is iterable, i.e. it implements the iterable protocol by
* providing a `Symbol.iterator` method.
*
* Arrays, strings, `Set`, `Map`, typed arrays, and generators are iterable;
* plain objects, `null`, and `undefined` are not.
*
* This function can be used as a TypeScript type predicate to narrow the type of
* `value` to `Iterable<unknown>`.
*
* @param value - The value to check.
* @returns `true` if `value` is iterable, `false` otherwise.
*
* @example
* isIterable([1, 2, 3]); // true
* isIterable('abc'); // true
* isIterable(new Set([1, 2, 3])); // true
* isIterable(new Map()); // true
* isIterable({ a: 1 }); // false
* isIterable(123); // false
* isIterable(null); // false
*/
function isIterable(value) {
	return value != null && typeof value[Symbol.iterator] === "function";
}
//#endregion
exports.isIterable = isIterable;
