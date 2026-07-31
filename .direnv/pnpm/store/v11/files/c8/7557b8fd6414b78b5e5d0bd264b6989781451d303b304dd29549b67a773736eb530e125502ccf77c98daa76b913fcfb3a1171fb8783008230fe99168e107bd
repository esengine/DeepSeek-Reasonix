import { uniqWith } from "./uniqWith.mjs";
//#region src/array/unionWith.ts
/**
* Creates an array of unique values from two given arrays based on a custom equality function.
*
* This function takes two arrays and a custom equality function, merges the arrays, and returns
* a new array containing only the unique values as determined by the custom equality function.
*
* @template T - The type of elements in the array.
* @param arr1 - The first array to merge and filter for unique values.
* @param arr2 - The second array to merge and filter for unique values.
* @param areItemsEqual - A custom function to determine if two elements are equal.
* It takes two arguments and returns `true` if the elements are considered equal, and `false` otherwise.
* @returns A new array of unique values based on the custom equality function.
*
* @example
* const array1 = [{ id: 1 }, { id: 2 }];
* const array2 = [{ id: 2 }, { id: 3 }];
* const areItemsEqual = (a, b) => a.id === b.id;
* const result = unionWith(array1, array2, areItemsEqual);
* // result will be [{ id: 1 }, { id: 2 }, { id: 3 }] since { id: 2 } is considered equal in both arrays
*/
function unionWith(arr1, arr2, areItemsEqual) {
	return uniqWith(arr1.concat(arr2), areItemsEqual);
}
//#endregion
export { unionWith };
