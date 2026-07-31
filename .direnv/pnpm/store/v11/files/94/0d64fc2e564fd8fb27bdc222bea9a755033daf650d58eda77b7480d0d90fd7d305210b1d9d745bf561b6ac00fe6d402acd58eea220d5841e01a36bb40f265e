import { isNull } from "../../predicate/isNull.mjs";
import { isUndefined } from "../../predicate/isUndefined.mjs";
import { iteratee } from "../util/iteratee.mjs";
import { isSymbol } from "../predicate/isSymbol.mjs";
import { identity } from "../function/identity.mjs";
import { isNil } from "../predicate/isNil.mjs";
import { isNaN } from "../predicate/isNaN.mjs";
const MAX_ARRAY_INDEX = 4294967294;
/**
* This method is like `sortedIndex` except that it accepts `iteratee`
* which is invoked for `value` and each element of `array` to compute their
* sort ranking. The iteratee is invoked with one argument: (value).
*
* @param array The sorted array to inspect.
* @param value The value to evaluate.
* @param iteratee The iteratee invoked per element.
* @returns Returns the index at which `value` should be inserted
*  into `array`.
* @example
* const objects = [{ 'n': 4 }, { 'n': 5 }]
* sortedIndexBy(objects, { 'n': 4 }, ({ n }) => n)
* // => 0
*/
function sortedIndexBy(array, value, iteratee$1 = identity, retHighest) {
	if (isNil(array) || array.length === 0) return 0;
	let low = 0;
	let high = array.length;
	const iterateeFunction = iteratee(iteratee$1);
	const transformedValue = iterateeFunction(value);
	const valIsNaN = isNaN(transformedValue);
	const valIsNull = isNull(transformedValue);
	const valIsSymbol = isSymbol(transformedValue);
	const valIsUndefined = isUndefined(transformedValue);
	while (low < high) {
		let setLow;
		const mid = Math.floor((low + high) / 2);
		const computed = iterateeFunction(array[mid]);
		const othIsDefined = !isUndefined(computed);
		const othIsNull = isNull(computed);
		const othIsReflexive = !isNaN(computed);
		const othIsSymbol = isSymbol(computed);
		if (valIsNaN) setLow = retHighest || othIsReflexive;
		else if (valIsUndefined) setLow = othIsReflexive && (retHighest || othIsDefined);
		else if (valIsNull) setLow = othIsReflexive && othIsDefined && (retHighest || !othIsNull);
		else if (valIsSymbol) setLow = othIsReflexive && othIsDefined && !othIsNull && (retHighest || !othIsSymbol);
		else if (othIsNull || othIsSymbol) setLow = false;
		else setLow = retHighest ? computed <= transformedValue : computed < transformedValue;
		if (setLow) low = mid + 1;
		else high = mid;
	}
	return Math.min(high, MAX_ARRAY_INDEX);
}
//#endregion
export { sortedIndexBy };
