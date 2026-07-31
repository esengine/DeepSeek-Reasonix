const require_remove = require("../../array/remove.js");
const require_identity = require("../../function/identity.js");
const require_iteratee = require("../util/iteratee.js");
//#region src/compat/array/remove.ts
/**
* Removes elements from an array based on various criteria.
*
* @param arr - The array to iterate over.
* @param shouldRemoveElement - The function invoked per iteration, a partial object, a property-value pair, or a key to match against each element.
* @returns Returns the modified array with the specified elements removed.
*
* @example
* // Using a predicate function
* const numbers = [1, 2, 3, 4, 5];
* remove(numbers, value => value % 2 === 0); // => [1, 3, 5]
*
* @example
* // Using a partial object
* const objects = [{ a: 1 }, { a: 2 }, { a: 3 }];
* remove(objects, { a: 1 }); // => [{ a: 2 }, { a: 3 }]
*
* @example
* // Using a property-value pair
* const objects = [{ a: 1 }, { a: 2 }, { a: 3 }];
* remove(objects, ['a', 1]); // => [{ a: 2 }, { a: 3 }]
*
* @example
* // Using a property key
* const objects = [{ a: 0 }, { a: 1 }];
* remove(objects, 'a'); // => [{ a: 0 }]
*/
function remove(arr, shouldRemoveElement = require_identity.identity) {
	return require_remove.remove(arr, require_iteratee.iteratee(shouldRemoveElement));
}
//#endregion
exports.remove = remove;
