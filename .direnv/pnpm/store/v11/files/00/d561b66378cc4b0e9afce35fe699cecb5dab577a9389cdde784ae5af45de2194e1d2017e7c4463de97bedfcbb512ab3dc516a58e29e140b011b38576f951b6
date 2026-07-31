const require_isEqualsSameValueZero = require("../../_internal/isEqualsSameValueZero.js");
require("../util/eq.js");
const require_isArrayLike = require("../predicate/isArrayLike.js");
const require_isObject = require("../predicate/isObject.js");
const require_isIndex = require("./isIndex.js");
//#region src/compat/_internal/isIterateeCall.ts
function isIterateeCall(value, index, object) {
	if (!require_isObject.isObject(object)) return false;
	if (typeof index === "number" && require_isArrayLike.isArrayLike(object) && require_isIndex.isIndex(index) && index < object.length || typeof index === "string" && index in object) return require_isEqualsSameValueZero.isEqualsSameValueZero(object[index], value);
	return false;
}
//#endregion
exports.isIterateeCall = isIterateeCall;
