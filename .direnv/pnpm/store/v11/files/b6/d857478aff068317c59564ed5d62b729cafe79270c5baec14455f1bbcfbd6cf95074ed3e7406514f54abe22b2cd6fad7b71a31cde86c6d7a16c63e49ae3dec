const require_isPlainObject = require("../predicate/isPlainObject.js");
const require_isDeepKey = require("../_internal/isDeepKey.js");
const require_cloneDeepWith = require("./cloneDeepWith.js");
const require_flatten = require("../array/flatten.js");
const require_unset = require("./unset.js");
const require_keysIn = require("./keysIn.js");
const require_getSymbolsIn = require("../_internal/getSymbolsIn.js");
//#region src/compat/object/omit.ts
/**
* Creates a new object with specified keys omitted.
*
* This function takes an object and a variable number of keys, and returns a new object that
* excludes the properties corresponding to the specified keys. Note that keys can be deep.
*
* Deep keys can be specified for keys.
*
* @template T - The type of object.
* @param obj - The object to omit keys from.
* @param keysArr - A variable number of keys to be omitted from the object.
* @returns A new object with the specified keys omitted.
*
* @example
* omit({ a: 1, b: 2, c: 3 }, 'a', 'b');
* // => { c: 3 }
*
* omit({ a: { b: 1, c: 2 }, d: 3 }, 'a.b', 'd');
* // => { a: { c: 2 } }
*/
function omit(obj, ...keysArr) {
	if (obj == null) return {};
	keysArr = require_flatten.flatten(keysArr);
	const result = cloneInOmit(obj, keysArr);
	for (let i = 0; i < keysArr.length; i++) {
		let keys = keysArr[i];
		switch (typeof keys) {
			case "object":
				if (!Array.isArray(keys)) keys = Array.from(keys);
				for (let j = 0; j < keys.length; j++) {
					const key = keys[j];
					require_unset.unset(result, key);
				}
				break;
			case "string":
			case "symbol":
			case "number":
				require_unset.unset(result, keys);
				break;
		}
	}
	return result;
}
function cloneInOmit(obj, keys) {
	if (keys.some((key) => Array.isArray(key) || require_isDeepKey.isDeepKey(key))) return deepCloneInOmit(obj);
	return shallowCloneInOmit(obj);
}
function shallowCloneInOmit(obj) {
	const result = {};
	const keysToCopy = [...require_keysIn.keysIn(obj), ...require_getSymbolsIn.getSymbolsIn(obj)];
	for (let i = 0; i < keysToCopy.length; i++) {
		const key = keysToCopy[i];
		result[key] = obj[key];
	}
	return result;
}
function deepCloneInOmit(obj) {
	const result = {};
	const keysToCopy = [...require_keysIn.keysIn(obj), ...require_getSymbolsIn.getSymbolsIn(obj)];
	for (let i = 0; i < keysToCopy.length; i++) {
		const key = keysToCopy[i];
		result[key] = require_cloneDeepWith.cloneDeepWith(obj[key], (valueToClone) => {
			if (require_isPlainObject.isPlainObject(valueToClone)) return;
			return valueToClone;
		});
	}
	return result;
}
//#endregion
exports.omit = omit;
