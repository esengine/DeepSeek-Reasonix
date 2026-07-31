import { unzip as unzip$1 } from "../../array/unzip.mjs";
import { isArray } from "../predicate/isArray.mjs";
import { isArrayLikeObject } from "../predicate/isArrayLikeObject.mjs";
//#region src/compat/array/unzip.ts
/**
* Gathers elements in the same position in an internal array
* from a grouped array of elements and returns them as a new array.
*
* @template T - The type of elements in the nested array.
* @param array - The nested array to unzip.
* @returns A new array of unzipped elements.
*
* @example
* const zipped = [['a', true, 1],['b', false, 2]];
* const result = unzip(zipped);
* // result will be [['a', 'b'], [true, false], [1, 2]]
*/
function unzip(array) {
	if (!isArrayLikeObject(array) || !array.length) return [];
	array = isArray(array) ? array : Array.from(array);
	array = array.filter((item) => isArrayLikeObject(item));
	return unzip$1(array);
}
//#endregion
export { unzip };
