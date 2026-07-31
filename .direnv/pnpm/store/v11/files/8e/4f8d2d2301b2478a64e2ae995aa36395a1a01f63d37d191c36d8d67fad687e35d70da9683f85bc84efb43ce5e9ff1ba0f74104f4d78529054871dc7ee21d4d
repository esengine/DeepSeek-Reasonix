import { uniqBy as uniqBy$1 } from "../../array/uniqBy.mjs";
import { combineEagerAndLazyFunctions } from "../_internal/lazy.mjs";
//#region src/fp/array/uniqBy.ts
/**
* Creates a function that removes duplicate values by mapped identity.
*
* The first value for each mapped key is preserved. The returned function is
* lazy-capable inside {@link pipe}.
*
* @template T - The type of elements in the array.
* @template U - The type of comparison keys returned by the mapper.
* @param mapper - Called with each value and index to produce a comparison key.
* @returns A function that maps the piped array to unique values.
*
* @example
* import { pipe, uniqBy } from 'es-toolkit/fp';
*
* pipe([{ id: 1 }, { id: 1 }, { id: 2 }], uniqBy(item => item.id));
* // => [{ id: 1 }, { id: 2 }]
*/
function uniqBy(mapper) {
	function uniqByEager(array) {
		return uniqBy$1(array, (item, index) => mapper(item, index));
	}
	const uniqByLazy = (emit) => {
		const seen = /* @__PURE__ */ new Set();
		let index = 0;
		return (value) => {
			const key = mapper(value, index++);
			if (seen.has(key)) return true;
			seen.add(key);
			return emit(value);
		};
	};
	return combineEagerAndLazyFunctions(uniqByEager, uniqByLazy);
}
//#endregion
export { uniqBy };
