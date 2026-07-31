//#region src/fp/array/findLast.ts
function findLast(predicate) {
	return function(array) {
		for (let index = array.length - 1; index >= 0; index--) {
			const value = array[index];
			if (predicate(value, index, array)) return value;
		}
	};
}
//#endregion
exports.findLast = findLast;
