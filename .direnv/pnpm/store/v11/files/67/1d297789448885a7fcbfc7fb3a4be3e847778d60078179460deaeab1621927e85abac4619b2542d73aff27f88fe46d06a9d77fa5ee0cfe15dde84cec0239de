import { keysIn } from "./keysIn.mjs";
import { mapToEntries } from "../_internal/mapToEntries.mjs";
import { setToEntries } from "../_internal/setToEntries.mjs";
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
	if (object instanceof Set) return setToEntries(object);
	if (object instanceof Map) return mapToEntries(object);
	const keys = keysIn(object);
	const result = new Array(keys.length);
	for (let i = 0; i < keys.length; i++) {
		const key = keys[i];
		result[i] = [key, object[key]];
	}
	return result;
}
//#endregion
export { toPairsIn };
