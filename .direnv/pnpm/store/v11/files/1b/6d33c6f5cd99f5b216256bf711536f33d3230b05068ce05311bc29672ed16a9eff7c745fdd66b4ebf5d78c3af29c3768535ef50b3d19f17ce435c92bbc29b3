//#region src/compat/_internal/getTag.ts
/**
* Gets the `toStringTag` of `value`.
*
* @private
* @param {T} value The value to query.
* @returns {string} Returns the `Object.prototype.toString.call` result.
*/
function getTag(value) {
	if (value == null) return value === void 0 ? "[object Undefined]" : "[object Null]";
	return Object.prototype.toString.call(value);
}
//#endregion
exports.getTag = getTag;
