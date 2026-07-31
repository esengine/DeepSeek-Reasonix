const require_toInteger = require("../util/toInteger.js");
//#region src/compat/function/before.ts
/**
* Creates a function that invokes `func`, with the `this` binding and arguments
* of the created function, while it's called less than `n` times. Subsequent
* calls to the created function return the result of the last `func` invocation.
*
* @template F - The type of the function to be invoked.
* @param n - The number of times the returned function is allowed to call `func` before stopping.
* - If `n` is 0, `func` will never be called.
* - If `n` is a positive integer, `func` will be called up to `n-1` times.
* @param func - The function to be called with the limit applied.
* @returns A new function that:
* - Tracks the number of calls.
* - Invokes `func` until the `n-1`-th call.
* - Returns last result of `func`, if `n` is reached.
* @throws {TypeError} - If `func` is not a function.
* @example
* let count = 0;
* const before3 = before(3, () => ++count);
*
* before3(); // => 1
* before3(); // => 2
* before3(); // => 2
*/
function before(n, func) {
	if (typeof func !== "function") throw new TypeError("Expected a function");
	let result;
	n = require_toInteger.toInteger(n);
	return function(...args) {
		if (--n > 0) result = func.apply(this, args);
		if (n <= 1 && func) func = void 0;
		return result;
	};
}
//#endregion
exports.before = before;
