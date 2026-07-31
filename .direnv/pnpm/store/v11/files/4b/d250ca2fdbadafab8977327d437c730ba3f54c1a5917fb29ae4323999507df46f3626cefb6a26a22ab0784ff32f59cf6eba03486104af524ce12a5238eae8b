//#region src/object/omit.ts
/**
* Creates a new object with specified keys omitted.
*
* This function takes an object and an array of keys, and returns a new object that
* excludes the properties corresponding to the specified keys.
*
* @template T - The type of object.
* @template K - The type of keys in object.
* @param obj - The object to omit keys from.
* @param keys - An array of keys to be omitted from the object.
* @returns A new object with the specified keys omitted.
*
* @example
* const obj = { a: 1, b: 2, c: 3 };
* const result = omit(obj, ['b', 'c']);
* // result will be { a: 1 }
*/
function omit(obj, keys) {
	const result = { ...obj };
	for (let i = 0; i < keys.length; i++) {
		const key = keys[i];
		delete result[key];
	}
	return result;
}
//#endregion
exports.omit = omit;
