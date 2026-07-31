const require_isBuffer = require("../../predicate/isBuffer.js");
const require_isArrayLike = require("../predicate/isArrayLike.js");
const require_isPrototype = require("../_internal/isPrototype.js");
const require_isTypedArray = require("../predicate/isTypedArray.js");
const require_times = require("../util/times.js");
//#region src/compat/object/keys.ts
/**
* Creates an array of the own enumerable property names of `object`.
*
* Non-object values are coerced to objects.
*
* @param object The object to query.
* @returns Returns the array of property names.
* @example
* function Foo() {
*   this.a = 1;
*   this.b = 2;
* }
* Foo.prototype.c = 3;
* keys(new Foo); // ['a', 'b'] (iteration order is not guaranteed)
*
* keys('hi'); // ['0', '1']
* keys([1, 2, 3]); // ['0', '1', '2']
* keys({ a: 1, b: 2 }); // ['a', 'b']
*/
function keys(object) {
	if (require_isArrayLike.isArrayLike(object)) return arrayLikeKeys(object);
	const result = Object.keys(Object(object));
	if (!require_isPrototype.isPrototype(object)) return result;
	return result.filter((key) => key !== "constructor");
}
function arrayLikeKeys(object) {
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
	const inheritedKeys = Object.keys(object).filter((key) => !filteredKeys.has(key));
	if (Array.isArray(object)) return [...indices, ...inheritedKeys];
	return [...indices.filter((index) => Object.hasOwn(object, index)), ...inheritedKeys];
}
//#endregion
exports.keys = keys;
