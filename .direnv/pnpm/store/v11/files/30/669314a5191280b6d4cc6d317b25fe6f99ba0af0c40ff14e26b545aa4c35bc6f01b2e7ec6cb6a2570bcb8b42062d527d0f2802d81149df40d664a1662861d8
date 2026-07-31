const require_isEqualsSameValueZero = require("../../_internal/isEqualsSameValueZero.js");
require("../util/eq.js");
const require_keysIn = require("./keysIn.js");
//#region src/compat/object/assignInWith.ts
/**
* Assigns properties from multiple source objects to a target object.
* You can provide a `getValueToAssign` function to determine what value will be assigned for each property.
*
* This function merges the properties of the source objects into the target object,
* including properties from the prototype chain. If a property in the source objects
* is equal to the corresponding property in the target object, it will not be overwritten.
*
* Unlike `assignIn`, this method accepts a `getValueToAssign` function that determines
* the final value to be assigned to each property in the target object. The return value
* of this function will be directly assigned to the corresponding property. This allows for
* more precise control over how properties are merged between objects. If not provided,
* the default behavior is equivalent to using the identity function (returning the source value).
*
* @param object - The target object to which properties will be assigned.
* @param sources - The source objects whose properties will be assigned to the target object.
* @returns The updated target object with properties from the source objects assigned.
*
* @example
* const target = { a: 1 };
* const result = assignInWith(target, { b: 2 }, { c: 3 }, function(objValue, srcValue) {
*   return objValue === undefined ? srcValue : objValue;
* });
* console.log(result); // Output: { a: 1, b: 2, c: 3 }
*/
function assignInWith(object, ...sources) {
	let getValueToAssign = sources[sources.length - 1];
	if (typeof getValueToAssign === "function") sources.pop();
	else getValueToAssign = void 0;
	for (let i = 0; i < sources.length; i++) assignInWithImpl(object, sources[i], getValueToAssign);
	return object;
}
function assignInWithImpl(object, source, getValueToAssign) {
	const keys = require_keysIn.keysIn(source);
	for (let i = 0; i < keys.length; i++) {
		const key = keys[i];
		const objValue = object[key];
		const srcValue = source[key];
		const newValue = getValueToAssign?.(objValue, srcValue, key, object, source) ?? srcValue;
		if (!(key in object) || !require_isEqualsSameValueZero.isEqualsSameValueZero(objValue, newValue)) object[key] = newValue;
	}
}
//#endregion
exports.assignInWith = assignInWith;
