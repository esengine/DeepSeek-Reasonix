const require_isNil = require("../../predicate/isNil.js");
const require_isNull = require("../../predicate/isNull.js");
const require_isSymbol = require("../../predicate/isSymbol.js");
const require_isNumber = require("../predicate/isNumber.js");
const require_sortedLastIndexBy = require("./sortedLastIndexBy.js");
const HALF_MAX_ARRAY_LENGTH = 2147483647;
/**
* Uses a binary search to determine the highest index at which `value`
* should be inserted into `array` in order to maintain its sort order.
*
* @category Array
* @param array The sorted array to inspect.
* @param value The value to evaluate.
* @returns Returns the index at which `value` should be inserted
*  into `array`.
* @example
* sortedLastIndex([4, 5, 5, 5, 6], 5)
* // => 4
*/
function sortedLastIndex(array, value) {
	if (require_isNil.isNil(array)) return 0;
	let high = array.length;
	if (!require_isNumber.isNumber(value) || Number.isNaN(value) || high > HALF_MAX_ARRAY_LENGTH) return require_sortedLastIndexBy.sortedLastIndexBy(array, value, (value) => value);
	let low = 0;
	while (low < high) {
		const mid = low + high >>> 1;
		const compute = array[mid];
		if (!require_isNull.isNull(compute) && !require_isSymbol.isSymbol(compute) && compute <= value) low = mid + 1;
		else high = mid;
	}
	return high;
}
//#endregion
exports.sortedLastIndex = sortedLastIndex;
