const require_isEqualsSameValueZero = require("../../_internal/isEqualsSameValueZero.js");
require("../util/eq.js");
const require_keys = require("./keys.js");
//#region src/compat/object/assign.ts
/**
* Assigns properties from multiple source objects to a target object.
*
* This function merges the properties of the source objects into the target object.
* If a property in the source objects is equal to the corresponding property in the target object,
* it will not be overwritten.
*
* @param object - The target object to which properties will be assigned.
* @param sources - The source objects whose properties will be assigned to the target object.
* @returns The updated target object with properties from the source objects assigned.
*
* @example
* const target = { a: 1 };
* const result = assign(target, { b: 2 }, { c: 3 });
* console.log(result); // Output: { a: 1, b: 2, c: 3 }
*/
function assign(object, ...sources) {
	for (let i = 0; i < sources.length; i++) assignImpl(object, sources[i]);
	return object;
}
function assignImpl(object, source) {
	const keys$1 = require_keys.keys(source);
	for (let i = 0; i < keys$1.length; i++) {
		const key = keys$1[i];
		if (!(key in object) || !require_isEqualsSameValueZero.isEqualsSameValueZero(object[key], source[key])) object[key] = source[key];
	}
}
//#endregion
exports.assign = assign;
