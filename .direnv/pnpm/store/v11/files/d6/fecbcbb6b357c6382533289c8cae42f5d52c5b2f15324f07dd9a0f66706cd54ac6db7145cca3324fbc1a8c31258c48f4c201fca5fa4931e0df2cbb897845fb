const require_uniqBy = require("../../array/uniqBy.js");
const require_ary = require("../../function/ary.js");
const require_identity = require("../../function/identity.js");
const require_iteratee = require("../util/iteratee.js");
const require_isArrayLikeObject = require("../predicate/isArrayLikeObject.js");
//#region src/compat/array/uniqBy.ts
function uniqBy(array, iteratee$1 = require_identity.identity) {
	if (!require_isArrayLikeObject.isArrayLikeObject(array)) return [];
	return require_uniqBy.uniqBy(Array.from(array), require_ary.ary(require_iteratee.iteratee(iteratee$1), 1));
}
//#endregion
exports.uniqBy = uniqBy;
