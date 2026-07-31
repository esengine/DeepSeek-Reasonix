const require_identity = require("../../function/identity.js");
const require_range = require("../../math/range.js");
const require_isArrayLike = require("../predicate/isArrayLike.js");
const require_iteratee = require("../util/iteratee.js");
//#region src/compat/array/map.ts
function map(collection, _iteratee) {
	if (!collection) return [];
	const keys = require_isArrayLike.isArrayLike(collection) || Array.isArray(collection) ? require_range.range(0, collection.length) : Object.keys(collection);
	const iteratee$1 = require_iteratee.iteratee(_iteratee ?? require_identity.identity);
	const result = new Array(keys.length);
	for (let i = 0; i < keys.length; i++) {
		const key = keys[i];
		const value = collection[key];
		result[i] = iteratee$1(value, key, collection);
	}
	return result;
}
//#endregion
exports.map = map;
