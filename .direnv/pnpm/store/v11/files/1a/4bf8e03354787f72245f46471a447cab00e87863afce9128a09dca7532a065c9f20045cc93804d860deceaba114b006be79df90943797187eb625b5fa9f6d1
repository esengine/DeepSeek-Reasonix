const require_unionWith = require("../../array/unionWith.js");
//#region src/fp/array/unionWith.ts
/**
* Creates a function that returns unique values using a custom equality function.
*
* Values from the piped array are considered before values from secondArray. The equality
* function decides whether two values represent the same item.
*
* @template T - The type of elements in the arrays.
* @param secondArray - Values to include after the piped array.
* @param areItemsEqual - Returns true when two values are equal.
* @returns A function that maps a readonly array to its union with secondArray.
*
* @example
* import { pipe, unionWith } from 'es-toolkit/fp';
*
* pipe([{ id: 1 }], unionWith([{ id: 1 }, { id: 2 }], (a, b) => a.id === b.id));
* // => [{ id: 1 }, { id: 2 }]
*/
function unionWith(secondArray, areItemsEqual) {
	return function(array) {
		return require_unionWith.unionWith(array, secondArray, areItemsEqual);
	};
}
//#endregion
exports.unionWith = unionWith;
