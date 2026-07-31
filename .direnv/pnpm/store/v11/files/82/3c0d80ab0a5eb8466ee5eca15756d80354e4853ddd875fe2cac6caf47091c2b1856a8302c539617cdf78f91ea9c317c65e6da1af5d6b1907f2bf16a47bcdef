const require_isBuffer = require("../../predicate/isBuffer.js");
const require_isArrayLike = require("../predicate/isArrayLike.js");
const require_isPrototype = require("../_internal/isPrototype.js");
const require_isTypedArray = require("../predicate/isTypedArray.js");
const require_times = require("../util/times.js");
//#region src/compat/object/keysIn.ts
/**
* This function retrieves the names of string-keyed properties from an object, including those inherited from its prototype.
*
* - If the value is not an object, it is converted to an object.
* - Array-like objects are treated like arrays.
* - Sparse arrays with some missing indices are treated like dense arrays.
* - If the value is `null` or `undefined`, an empty array is returned.
* - When handling prototype objects, the `constructor` property is excluded from the results.
*
* @param [object] - The object to inspect for keys.
* @returns An array of string keys from the object.
*
* @example
* const obj = { a: 1, b: 2 };
* console.log(keysIn(obj)); // ['a', 'b']
*
* const arr = [1, 2, 3];
* console.log(keysIn(arr)); // ['0', '1', '2']
*
* function Foo() {}
* Foo.prototype.a = 1;
* console.log(keysIn(new Foo())); // ['a']
*/
function keysIn(object) {
	if (object == null) return [];
	switch (typeof object) {
		case "object":
		case "function":
			if (require_isArrayLike.isArrayLike(object)) return arrayLikeKeysIn(object);
			if (require_isPrototype.isPrototype(object)) return prototypeKeysIn(object);
			return keysInImpl(object);
		default: return keysInImpl(Object(object));
	}
}
function keysInImpl(object) {
	const result = [];
	for (const key in object) result.push(key);
	return result;
}
function prototypeKeysIn(object) {
	return keysInImpl(object).filter((key) => key !== "constructor");
}
function arrayLikeKeysIn(object) {
	const indices = require_times.times(object.length, (index) => `${index}`);
	const filteredKeys = new Set(indices);
	if (require_isBuffer.isBuffer(object)) {
		filteredKeys.add("offset");
		filteredKeys.add("parent");
	}
	if (require_isTypedArray.isTypedArray(object)) {
		filteredKeys.add("buffer");
		filteredKeys.add("byteLength");
		filteredKeys.add("byteOffset");
	}
	const inheritedKeys = keysInImpl(object).filter((key) => !filteredKeys.has(key));
	if (Array.isArray(object)) return [...indices, ...inheritedKeys];
	return [...indices.filter((index) => Object.hasOwn(object, index)), ...inheritedKeys];
}
//#endregion
exports.keysIn = keysIn;
