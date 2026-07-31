const require_isNull = require("../../predicate/isNull.js");
const require_isUndefined = require("../../predicate/isUndefined.js");
const require_iteratee = require("../util/iteratee.js");
const require_isSymbol = require("../predicate/isSymbol.js");
const require_identity = require("../function/identity.js");
const require_isNil = require("../predicate/isNil.js");
const require_isNaN = require("../predicate/isNaN.js");
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
function sortedIndexBy(array, value, iteratee$1 = require_identity.identity, retHighest) {
	if (require_isNil.isNil(array) || array.length === 0) return 0;
	let low = 0;
	let high = array.length;
	const iterateeFunction = require_iteratee.iteratee(iteratee$1);
	const transformedValue = iterateeFunction(value);
	const valIsNaN = require_isNaN.isNaN(transformedValue);
	const valIsNull = require_isNull.isNull(transformedValue);
	const valIsSymbol = require_isSymbol.isSymbol(transformedValue);
	const valIsUndefined = require_isUndefined.isUndefined(transformedValue);
	while (low < high) {
		let setLow;
		const mid = Math.floor((low + high) / 2);
		const computed = iterateeFunction(array[mid]);
		const othIsDefined = !require_isUndefined.isUndefined(computed);
		const othIsNull = require_isNull.isNull(computed);
		const othIsReflexive = !require_isNaN.isNaN(computed);
		const othIsSymbol = require_isSymbol.isSymbol(computed);
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
exports.sortedIndexBy = sortedIndexBy;
