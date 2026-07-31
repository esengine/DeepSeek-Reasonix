const require_drop = require("../../array/drop.js");
const require_toArray = require("../_internal/toArray.js");
const require_isArrayLike = require("../predicate/isArrayLike.js");
const require_toInteger = require("../util/toInteger.js");
//#region src/compat/array/drop.ts
function drop(array, itemsCount = 1, guard) {
	if (!require_isArrayLike.isArrayLike(array)) return [];
	itemsCount = guard ? 1 : require_toInteger.toInteger(itemsCount);
	return require_drop.drop(require_toArray.toArray(array), itemsCount);
}
//#endregion
exports.drop = drop;
