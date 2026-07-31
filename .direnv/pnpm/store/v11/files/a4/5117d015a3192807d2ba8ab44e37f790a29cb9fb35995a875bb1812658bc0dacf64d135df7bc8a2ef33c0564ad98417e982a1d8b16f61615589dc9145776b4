//#region src/object/pick.ts
/**
* Creates a new object composed of the picked object properties.
*
* This function takes an object and an array of keys, and returns a new object that
* includes only the properties corresponding to the specified keys.
*
* @template T - The type of object.
* @template K - The type of keys in object.
* @param obj - The object to pick keys from.
* @param keys - An array of keys to be picked from the object.
* @returns A new object with the specified keys picked.
*
* @example
* const obj = { a: 1, b: 2, c: 3 };
* const result = pick(obj, ['a', 'c']);
* // result will be { a: 1, c: 3 }
*/
function pick(obj, keys) {
	const result = {};
	for (let i = 0; i < keys.length; i++) {
		const key = keys[i];
		if (Object.hasOwn(obj, key)) result[key] = obj[key];
	}
	return result;
}
//#endregion
export { pick };
