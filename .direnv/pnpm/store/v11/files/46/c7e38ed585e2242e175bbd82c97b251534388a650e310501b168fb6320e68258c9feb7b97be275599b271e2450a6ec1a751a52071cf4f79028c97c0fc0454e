import { isBuffer } from "../../predicate/isBuffer.mjs";
import { isArrayLike } from "../predicate/isArrayLike.mjs";
import { isPrototype } from "../_internal/isPrototype.mjs";
import { isTypedArray } from "../predicate/isTypedArray.mjs";
import { times } from "../util/times.mjs";
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
	if (isArrayLike(object)) return arrayLikeKeys(object);
	const result = Object.keys(Object(object));
	if (!isPrototype(object)) return result;
	return result.filter((key) => key !== "constructor");
}
function arrayLikeKeys(object) {
	const indices = times(object.length, (index) => `${index}`);
	const filteredKeys = new Set(indices);
	if (isBuffer(object)) {
		filteredKeys.add("offset");
		filteredKeys.add("parent");
	}
	if (isTypedArray(object)) {
		filteredKeys.add("buffer");
		filteredKeys.add("byteLength");
		filteredKeys.add("byteOffset");
	}
	const inheritedKeys = Object.keys(object).filter((key) => !filteredKeys.has(key));
	if (Array.isArray(object)) return [...indices, ...inheritedKeys];
	return [...indices.filter((index) => Object.hasOwn(object, index)), ...inheritedKeys];
}
//#endregion
export { keys };
