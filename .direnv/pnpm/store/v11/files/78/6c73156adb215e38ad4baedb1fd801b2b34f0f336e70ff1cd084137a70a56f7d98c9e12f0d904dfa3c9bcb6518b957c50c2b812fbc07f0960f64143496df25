import { xorBy as xorBy$1 } from "../../array/xorBy.mjs";
//#region src/fp/array/xorBy.ts
/**
* Creates a function that returns the symmetric difference by mapped identity.
*
* The mapper is used to decide which values appear in exactly one array, matching the main
* {@link xorBy} behavior.
*
* @template T - The type of elements in the arrays.
* @template U - The type of comparison keys returned by the mapper.
* @param secondArray - Values to compare with the piped array.
* @param mapper - Called for values from both arrays to produce comparison keys.
* @returns A function that maps a readonly array to its symmetric difference.
*
* @example
* import { pipe, xorBy } from 'es-toolkit/fp';
*
* pipe([{ id: 1 }, { id: 2 }], xorBy([{ id: 2 }, { id: 3 }], item => item.id));
* // => [{ id: 1 }, { id: 3 }]
*/
function xorBy(secondArray, mapper) {
	return function(array) {
		return xorBy$1(array, secondArray, mapper);
	};
}
//#endregion
export { xorBy };
