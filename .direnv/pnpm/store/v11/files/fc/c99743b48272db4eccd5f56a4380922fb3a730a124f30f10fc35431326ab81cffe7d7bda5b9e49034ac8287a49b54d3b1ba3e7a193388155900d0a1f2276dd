import { omit as omit$1 } from "../../object/omit.mjs";
//#region src/fp/object/omit.ts
/**
* Creates a function that builds a new object with the given `keys` removed from
* the input object. Use it with {@link pipe}.
*
* @template T - The type of the input object.
* @template K - The union of keys to omit.
* @param keys - The keys to exclude from the new object.
* @returns A function that maps an object `T` to `Omit<T, K>`.
*
* @example
* import { pipe, omit } from 'es-toolkit/fp';
*
* pipe({ a: 1, b: 2, c: 3 }, omit(['b', 'c'])); // => { a: 1 }
*/
function omit(keys) {
	return function(obj) {
		return omit$1(obj, keys);
	};
}
//#endregion
export { omit };
