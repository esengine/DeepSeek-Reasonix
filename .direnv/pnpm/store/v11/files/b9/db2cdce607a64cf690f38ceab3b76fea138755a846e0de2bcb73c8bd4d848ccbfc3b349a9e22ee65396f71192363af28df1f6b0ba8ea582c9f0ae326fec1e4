const require_isArrayLike = require("../predicate/isArrayLike.js");
const require_iteratee = require("../util/iteratee.js");
//#region src/compat/array/countBy.ts
function countBy(collection, iteratee$1) {
	if (collection == null) return {};
	const array = require_isArrayLike.isArrayLike(collection) ? Array.from(collection) : Object.values(collection);
	const mapper = require_iteratee.iteratee(iteratee$1 ?? void 0);
	const result = Object.create(null);
	for (let i = 0; i < array.length; i++) {
		const item = array[i];
		const key = mapper(item);
		result[key] = (result[key] ?? 0) + 1;
	}
	return result;
}
//#endregion
exports.countBy = countBy;
