const require_isFunction = require("../../predicate/isFunction.js");
const require_iteratee = require("./iteratee.js");
//#region src/compat/util/cond.ts
function cond(pairs) {
	const length = pairs.length;
	const processedPairs = pairs.map((pair) => {
		const predicate = pair[0];
		const func = pair[1];
		if (!require_isFunction.isFunction(func)) throw new TypeError("Expected a function");
		return [require_iteratee.iteratee(predicate), func];
	});
	return function(...args) {
		for (let i = 0; i < length; i++) {
			const pair = processedPairs[i];
			const predicate = pair[0];
			const func = pair[1];
			if (predicate.apply(this, args)) return func.apply(this, args);
		}
	};
}
//#endregion
exports.cond = cond;
