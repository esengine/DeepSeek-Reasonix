import { combineEagerAndLazyFunctions, createLazyFunction } from "../_internal/lazy.mjs";
//#region src/fp/array/filter.ts
function filter(predicate) {
	function filterEager(array) {
		return array.filter(predicate);
	}
	return combineEagerAndLazyFunctions(filterEager, createLazyFunction((value, index, emit) => {
		if (predicate(value, index)) emit(value);
	}));
}
//#endregion
export { filter };
