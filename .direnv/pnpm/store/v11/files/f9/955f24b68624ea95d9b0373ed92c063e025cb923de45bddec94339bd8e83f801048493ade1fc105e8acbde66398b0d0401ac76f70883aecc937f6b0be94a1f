const require_get = require("./get.js");
//#region src/compat/object/property.ts
/**
* Creates a function that returns the value at a given path of an object.
*
* @template T - The type of object.
* @template R - The type of the value to return.
* @param path - The path of the property to get.
* @returns Returns a new function that takes an object and returns the value at the specified path.
*
* @example
* const getObjectValue = property('a.b.c');
* const result = getObjectValue({ a: { b: { c: 3 } } });
* console.log(result); // => 3
*
* @example
* const getObjectValue = property(['a', 'b', 'c']);
* const result = getObjectValue({ a: { b: { c: 3 } } });
* console.log(result); // => 3
*/
function property(path) {
	return function(object) {
		return require_get.get(object, path);
	};
}
//#endregion
exports.property = property;
