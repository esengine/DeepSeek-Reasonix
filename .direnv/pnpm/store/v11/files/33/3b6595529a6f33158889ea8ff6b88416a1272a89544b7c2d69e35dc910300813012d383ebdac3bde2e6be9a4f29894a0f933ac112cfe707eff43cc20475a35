const require_keysIn = require("./keysIn.js");
//#region src/compat/object/valuesIn.ts
/**
* Retrieves the values from an object, including those inherited from its prototype.
*
* - If the value is not an object, it is converted to an object.
* - Array-like objects are treated like arrays.
* - Sparse arrays with some missing indices are treated like dense arrays.
* - If the value is `null` or `undefined`, an empty array is returned.
* - When handling prototype objects, the `constructor` property is excluded from the results.
*
* @param object The object to query.
* @returns Returns an array of property values.
* @example
* const obj = { a: 1, b: 2, c: 3 };
* valuesIn(obj); // => [1, 2, 3]
*/
function valuesIn(object) {
	const keys = require_keysIn.keysIn(object);
	const result = new Array(keys.length);
	for (let i = 0; i < keys.length; i++) result[i] = object[keys[i]];
	return result;
}
//#endregion
exports.valuesIn = valuesIn;
