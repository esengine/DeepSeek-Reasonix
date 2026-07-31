const require_isArrayLike = require("../predicate/isArrayLike.js");
const require_get = require("./get.js");
const require_has = require("./has.js");
const require_isNil = require("../predicate/isNil.js");
const require_set = require("./set.js");
//#region src/compat/object/pick.ts
/**
* Creates a new object composed of the picked object properties.
*
* This function takes an object and an array of keys, and returns a new object that
* includes only the properties corresponding to the specified keys.
*
* @template T - The type of object.
* @template U - The type of keys to pick.
* @param object - The object to pick keys from.
* @param props - An array of keys to be picked from the object. received keys goes through a flattening process before being used.
* @returns A new object with the specified keys picked.
*
* @example
* const obj = { a: 1, b: 2, c: 3 };
* const result = pick(obj, ['a', 'c']);
* // result will be { a: 1, c: 3 }
*
* // each path can be passed individually as an argument
* const obj = { a: 1, b: 2, c: 3 };
* const result = pick(obj, 'a', 'c');
* // result will be { a: 1, c: 3 }
*
* // pick a key over a path
* const obj = { 'a.b': 1, a: { b: 2 } };
* const result = pick(obj, 'a.b');
* // result will be { 'a.b': 1 }
*/
function pick(object, ...keysArr) {
	if (require_isNil.isNil(object)) return {};
	const result = {};
	for (let i = 0; i < keysArr.length; i++) {
		let keys = keysArr[i];
		switch (typeof keys) {
			case "object":
				if (!Array.isArray(keys)) if (require_isArrayLike.isArrayLike(keys)) keys = Array.from(keys);
				else keys = [keys];
				break;
			case "string":
			case "symbol":
			case "number":
				keys = [keys];
				break;
		}
		for (const key of keys) {
			const value = require_get.get(object, key);
			if (value === void 0 && !require_has.has(object, key)) continue;
			if (typeof key === "string" && Object.hasOwn(object, key)) result[key] = object[key];
			else require_set.set(result, key, value);
		}
	}
	return result;
}
//#endregion
exports.pick = pick;
