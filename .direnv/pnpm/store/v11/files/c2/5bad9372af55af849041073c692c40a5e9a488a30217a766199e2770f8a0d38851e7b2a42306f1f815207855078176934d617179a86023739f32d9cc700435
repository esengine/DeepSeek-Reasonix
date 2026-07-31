const require_toArray = require("../_internal/toArray.js");
const require_iteratee = require("../util/iteratee.js");
const require_isArrayLikeObject = require("../predicate/isArrayLikeObject.js");
const require_identity = require("../function/identity.js");
const require_negate = require("../function/negate.js");
//#region src/compat/array/takeWhile.ts
/**
* Creates a slice of the array with elements taken from the beginning while the specified predicate is satisfied.
* If no predicate is provided, the identity function is used by default.
* If the array is `null` or `undefined`, returns an empty array.
*
* @template T
* @param array - The array to process.
* @param [predicate] - The condition used to determine elements to include. Can be:
* - A function invoked per iteration.
* - A partial object to match properties.
* - A key-value pair as a tuple.
* - A property key to check for truthy values.
* Defaults to the identity function if not provided.
* @returns A slice of the array with elements taken from the beginning or an empty array if `array` is `null` or `undefined`.
*
* @example
* // Using a predicate function
* const items = [1, 2, 3, 4, 5];
* const result = takeWhile(items, (item) => item < 3);
* console.log(result); // [1, 2]
*
* // Using a partial object
* const items2 = [{ id: 30 }, { id: 20 }, { id: 10 }];
* const result2 = takeWhile(items2, { id: 30 });
* console.log(result2); // [{ id: 30 }]
*
* // Using a key-value pair
* const items3 = [{ name: 'Alice' }, { name: 'Bob' }, { name: 'Alice' }];
* const result3 = takeWhile(items3, ['name', 'Alice']);
* console.log(result3); // [{ name: 'Alice' }]
*
* // Using a property key
* const items4 = [{ active: true }, { active: true }, { active: false }];
* const result4 = takeWhile(items4, 'active');
* console.log(result4); // [{ active: true }, { active: true }]
*
* // No predicate provided
* const items5 = [true, false];
* const result5 = takeWhile(items5);
* console.log(result5); // [true]
*
* // null or undefined array
* const result6 = takeWhile(null);
* console.log(result6); // []
**/
function takeWhile(array, predicate) {
	if (!require_isArrayLikeObject.isArrayLikeObject(array)) return [];
	const _array = require_toArray.toArray(array);
	const index = _array.findIndex(require_negate.negate(require_iteratee.iteratee(predicate ?? require_identity.identity)));
	return index === -1 ? _array : _array.slice(0, index);
}
//#endregion
exports.takeWhile = takeWhile;
