import { uniqWith as uniqWith$1 } from "../../array/uniqWith.mjs";
import { combineEagerAndLazyFunctions } from "../_internal/lazy.mjs";
//#region src/fp/array/uniqWith.ts
/**
* Creates a function that removes duplicate values using a custom equality function.
*
* The first value in each equality group is preserved. The returned function is
* lazy-capable inside {@link pipe}.
*
* @template T - The type of elements in the array.
* @param areItemsEqual - Returns true when two values should be treated as equal.
* @returns A function that maps the piped array to unique values.
*
* @example
* import { pipe, uniqWith } from 'es-toolkit/fp';
*
* pipe([{ id: 1 }, { id: 1 }, { id: 2 }], uniqWith((a, b) => a.id === b.id));
* // => [{ id: 1 }, { id: 2 }]
*/
function uniqWith(areItemsEqual) {
	function uniqWithEager(array) {
		return uniqWith$1(array, areItemsEqual);
	}
	const uniqWithLazy = (emit) => {
		const seen = [];
		return (value) => {
			if (seen.some((item) => areItemsEqual(item, value))) return true;
			seen.push(value);
			return emit(value);
		};
	};
	return combineEagerAndLazyFunctions(uniqWithEager, uniqWithLazy);
}
//#endregion
export { uniqWith };
