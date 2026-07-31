import { isNil } from "../../predicate/isNil.mjs";
import { isNull } from "../../predicate/isNull.mjs";
import { isSymbol } from "../../predicate/isSymbol.mjs";
import { isNumber } from "../predicate/isNumber.mjs";
import { sortedIndexBy } from "./sortedIndexBy.mjs";
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
	if (isNil(array)) return 0;
	let low = 0;
	let high = array.length;
	if (isNumber(value) && value === value && high <= HALF_MAX_ARRAY_LENGTH) {
		while (low < high) {
			const mid = low + high >>> 1;
			const compute = array[mid];
			if (!isNull(compute) && !isSymbol(compute) && compute < value) low = mid + 1;
			else high = mid;
		}
		return high;
	}
	return sortedIndexBy(array, value, (value) => value);
}
//#endregion
export { sortedIndex };
