const require_isNil = require("../../predicate/isNil.js");
const require_isArrayLike = require("../predicate/isArrayLike.js");
//#region src/compat/array/size.ts
function size(target) {
	if (require_isNil.isNil(target)) return 0;
	if (require_isArrayLike.isArrayLike(target)) return target.length;
	if (target instanceof Map || target instanceof Set) return target.size;
	return Object.keys(target).length;
}
//#endregion
exports.size = size;
