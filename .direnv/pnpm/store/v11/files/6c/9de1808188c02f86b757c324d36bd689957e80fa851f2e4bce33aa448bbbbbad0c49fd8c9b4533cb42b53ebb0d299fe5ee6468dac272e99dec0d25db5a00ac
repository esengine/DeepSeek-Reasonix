import { isNil } from "../../predicate/isNil.mjs";
import { isNull } from "../../predicate/isNull.mjs";
import { isSymbol } from "../../predicate/isSymbol.mjs";
import { isNumber } from "../predicate/isNumber.mjs";
import { sortedLastIndexBy } from "./sortedLastIndexBy.mjs";
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
	if (isNil(array)) return 0;
	let high = array.length;
	if (!isNumber(value) || Number.isNaN(value) || high > HALF_MAX_ARRAY_LENGTH) return sortedLastIndexBy(array, value, (value) => value);
	let low = 0;
	while (low < high) {
		const mid = low + high >>> 1;
		const compute = array[mid];
		if (!isNull(compute) && !isSymbol(compute) && compute <= value) low = mid + 1;
		else high = mid;
	}
	return high;
}
//#endregion
export { sortedLastIndex };
