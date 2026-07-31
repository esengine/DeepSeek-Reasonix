import { iteratee } from "../util/iteratee.mjs";
//#region src/compat/math/sumBy.ts
/**
* Computes the sum of the values that are returned by the `iteratee` function.
*
* It does not coerce values to `number`.
*
* @template T - The type of the array elements.
* @param array - The array to iterate over.
* @param iteratee - The function invoked per iteration.
* @returns Returns the sum.
*
* @example
* sumBy([1, undefined, 2], value => value); // => 3
* sumBy(null); // => 0
* sumBy(undefined); // => 0
* sumBy([1, 2, 3]); // => 6
* sumBy([1n, 2n, 3n]); // => 6n
* sumBy([{ a: "1" }, { a: "2" }], object => object.a); // => "12"
*/
function sumBy(array, iteratee$1) {
	if (!array || !array.length) return 0;
	if (iteratee$1 != null) iteratee$1 = iteratee(iteratee$1);
	let result = void 0;
	for (let i = 0; i < array.length; i++) {
		const current = iteratee$1 ? iteratee$1(array[i]) : array[i];
		if (current !== void 0) if (result === void 0) result = current;
		else result += current;
	}
	return result;
}
//#endregion
export { sumBy };
