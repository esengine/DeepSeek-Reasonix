import { keys } from "./keys.mjs";
import { mapToEntries } from "../_internal/mapToEntries.mjs";
import { setToEntries } from "../_internal/setToEntries.mjs";
//#region src/compat/object/toPairs.ts
/**
* Creates an array of key-value pairs from an object, set, or map.
*
* @template T
* @param object - The object, set, or map to query.
* @returns Returns the array of key-value pairs.
*
* @example
* const object = { a: 1, b: 2 };
* toPairs(object); // [['a', 1], ['b', 2]]
*
* const set = new Set([1, 2]);
* toPairs(set); // [[1, 1], [2, 2]]
*
* const map = new Map();
* map.set('a', 1);
* map.set('b', 2);
* toPairs(map); // [['a', 1], ['b', 2]]
*/
function toPairs(object) {
	if (object == null) return [];
	if (object instanceof Set) return setToEntries(object);
	if (object instanceof Map) return mapToEntries(object);
	const keys$1 = keys(object);
	const result = new Array(keys$1.length);
	for (let i = 0; i < keys$1.length; i++) {
		const key = keys$1[i];
		result[i] = [key, object[key]];
	}
	return result;
}
//#endregion
export { toPairs };
