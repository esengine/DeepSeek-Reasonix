const require_isArrayLikeObject = require("../predicate/isArrayLikeObject.js");
//#region src/compat/_internal/flattenArrayLike.ts
function flattenArrayLike(values) {
	const result = [];
	for (let i = 0; i < values.length; i++) {
		const arrayLike = values[i];
		if (!require_isArrayLikeObject.isArrayLikeObject(arrayLike)) continue;
		for (let j = 0; j < arrayLike.length; j++) result.push(arrayLike[j]);
	}
	return result;
}
//#endregion
exports.flattenArrayLike = flattenArrayLike;
