import { countBy as countBy$1 } from "../../array/countBy.mjs";
//#region src/fp/array/countBy.ts
/**
* Creates a function that counts values by a derived key.
*
* The mapper receives each value, its index, and the full input array. The returned object
* uses mapper results as keys and occurrence counts as values.
*
* @template T - The type of elements in the array.
* @template K - The property-key type produced by the mapper.
* @param mapper - Called with each value, index, and array to produce a key.
* @returns A function that maps a readonly array to counts by key.
*
* @example
* import { countBy, pipe } from 'es-toolkit/fp';
*
* pipe([1, 2, 3, 4, 5], countBy(value => (value % 2 === 0 ? 'even' : 'odd')));
* // => { odd: 3, even: 2 }
*/
function countBy(mapper) {
	return function(array) {
		return countBy$1(array, mapper);
	};
}
//#endregion
export { countBy };
