const require_lazy = require("../_internal/lazy.js");
//#region src/fp/array/filter.ts
function filter(predicate) {
	function filterEager(array) {
		return array.filter(predicate);
	}
	const filterLazy = require_lazy.createLazyFunction((value, index, emit) => {
		if (predicate(value, index)) emit(value);
	});
	return require_lazy.combineEagerAndLazyFunctions(filterEager, filterLazy);
}
//#endregion
exports.filter = filter;
