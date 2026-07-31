import { pick as pick$1 } from "../../object/pick.mjs";
//#region src/fp/object/pick.ts
/**
* Creates a function that builds a new object containing only the given `keys`
* from the input object. Keys that are absent on the input are skipped. Use it
* with {@link pipe}.
*
* @template T - The type of the input object.
* @template K - The union of keys to pick.
* @param keys - The keys to copy into the new object.
* @returns A function that maps an object `T` to `Pick<T, K>`.
*
* @example
* import { pipe, pick } from 'es-toolkit/fp';
*
* pipe({ a: 1, b: 2, c: 3 }, pick(['a', 'c'])); // => { a: 1, c: 3 }
*/
function pick(keys) {
	return function(obj) {
		return pick$1(obj, keys);
	};
}
//#endregion
export { pick };
