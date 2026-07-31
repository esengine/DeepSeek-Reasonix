const require_keysIn = require("./keysIn.js");
const require_mapToEntries = require("../_internal/mapToEntries.js");
const require_setToEntries = require("../_internal/setToEntries.js");
//#region src/compat/object/toPairsIn.ts
/**
* Creates an array of key-value pairs from an object, set, or map, including inherited properties.
*
* @param object The object, set, or map to query.
* @returns Returns the array of key-value pairs.
* @example
* const object = { a: 1, b: 2 };
* toPairsIn(object); // [['a', 1], ['b', 2]]
*
* const set = new Set([1, 2]);
* toPairsIn(set); // [[1, 1], [2, 2]]
*
* const map = new Map();
* map.set('a', 1);
* map.set('b', 2);
* toPairsIn(map); // [['a', 1], ['b', 2]]
*/
function toPairsIn(object) {
	if (object == null) return [];
	if (object instanceof Set) return require_setToEntries.setToEntries(object);
	if (object instanceof Map) return require_mapToEntries.mapToEntries(object);
	const keys = require_keysIn.keysIn(object);
	const result = new Array(keys.length);
	for (let i = 0; i < keys.length; i++) {
		const key = keys[i];
		result[i] = [key, object[key]];
	}
	return result;
}
//#endregion
exports.toPairsIn = toPairsIn;
