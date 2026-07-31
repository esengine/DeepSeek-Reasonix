const require_isNil = require("../../predicate/isNil.js");
const require_isNull = require("../../predicate/isNull.js");
const require_isSymbol = require("../../predicate/isSymbol.js");
const require_isNumber = require("../predicate/isNumber.js");
const require_sortedIndexBy = require("./sortedIndexBy.js");
const HALF_MAX_ARRAY_LENGTH = 2147483647;
/**
* Uses a binary search to determine the lowest index at which `value`
* should be inserted into `array` in order to maintain its sort order.
*
* @category Array
* @param array The sorted array to inspect.
* @param value The value to evaluate.
* @returns Returns the index at which `value` should be inserted
*  into `array`.
* @example
* sortedIndex([30, 50], 40)
* // => 1
*/
function sortedIndex(array, value) {
	if (require_isNil.isNil(array)) return 0;
	let low = 0;
	let high = array.length;
	if (require_isNumber.isNumber(value) && value === value && high <= HALF_MAX_ARRAY_LENGTH) {
		while (low < high) {
			const mid = low + high >>> 1;
			const compute = array[mid];
			if (!require_isNull.isNull(compute) && !require_isSymbol.isSymbol(compute) && compute < value) low = mid + 1;
			else high = mid;
		}
		return high;
	}
	return require_sortedIndexBy.sortedIndexBy(array, value, (value) => value);
}
//#endregion
exports.sortedIndex = sortedIndex;
