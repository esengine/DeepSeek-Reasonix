import { differenceWith } from "./differenceWith.mjs";
import { intersectionWith } from "./intersectionWith.mjs";
import { unionWith } from "./unionWith.mjs";
//#region src/array/xorWith.ts
/**
* Computes the symmetric difference between two arrays using a custom equality function.
* The symmetric difference is the set of elements which are in either of the arrays,
* but not in their intersection.
*
* @template T - Type of elements in the input arrays.
*
* @param arr1 - The first array.
* @param arr2 - The second array.
* @param areElementsEqual - The custom equality function to compare elements.
* @returns An array containing the elements that are present in either `arr1` or `arr2` but not in both, based on the custom equality function.
*
* @example
* // Custom equality function for objects with an 'id' property
* const areObjectsEqual = (a, b) => a.id === b.id;
* xorWith([{ id: 1 }, { id: 2 }], [{ id: 2 }, { id: 3 }], areObjectsEqual);
* // Returns [{ id: 1 }, { id: 3 }]
*/
function xorWith(arr1, arr2, areElementsEqual) {
	return differenceWith(unionWith(arr1, arr2, areElementsEqual), intersectionWith(arr1, arr2, areElementsEqual), areElementsEqual);
}
//#endregion
export { xorWith };
