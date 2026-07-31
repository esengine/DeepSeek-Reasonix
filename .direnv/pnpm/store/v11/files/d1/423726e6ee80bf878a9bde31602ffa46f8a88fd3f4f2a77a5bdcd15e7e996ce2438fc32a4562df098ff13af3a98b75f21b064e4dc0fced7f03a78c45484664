import { xorWith as xorWith$1 } from "../../array/xorWith.mjs";
//#region src/fp/array/xorWith.ts
/**
* Creates a function that returns the symmetric difference using a custom equality function.
*
* The equality function decides whether two values represent the same item, matching the
* main {@link xorWith} behavior.
*
* @template T - The type of elements in the arrays.
* @param secondArray - Values to compare with the piped array.
* @param areItemsEqual - Returns true when two values are equal.
* @returns A function that maps a readonly array to its symmetric difference.
*
* @example
* import { pipe, xorWith } from 'es-toolkit/fp';
*
* pipe([{ id: 1 }, { id: 2 }], xorWith([{ id: 2 }, { id: 3 }], (a, b) => a.id === b.id));
* // => [{ id: 1 }, { id: 3 }]
*/
function xorWith(secondArray, areItemsEqual) {
	return function(array) {
		return xorWith$1(array, secondArray, areItemsEqual);
	};
}
//#endregion
export { xorWith };
