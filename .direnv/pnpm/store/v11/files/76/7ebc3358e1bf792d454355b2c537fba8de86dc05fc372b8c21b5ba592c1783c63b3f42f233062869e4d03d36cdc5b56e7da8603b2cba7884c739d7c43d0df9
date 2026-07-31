const require_flatten = require("../../array/flatten.js");
const require_flowRight = require("../../function/flowRight.js");
//#region src/compat/function/flowRight.ts
/**
* Creates a new function that executes the given functions in sequence from right to left. The return value of the previous function is passed as an argument to the next function.
*
* The `this` context of the returned function is also passed to the functions provided as parameters.
*
* This method is like `flow` except that it creates a function that invokes the given functions from right to left.
*
* @param funcs The functions to invoke.
* @returns Returns the new composite function.
*
* @example
* const add = (x: number, y: number) => x + y;
* const square = (n: number) => n * n;
* const double = (n: number) => n * 2;
*
* const combined = flowRight(double, [square, add]);
* console.log(combined(1, 2)); // 18
*/
function flowRight(...funcs) {
	const flattenFuncs = require_flatten.flatten(funcs, 1);
	if (flattenFuncs.some((func) => typeof func !== "function")) throw new TypeError("Expected a function");
	return require_flowRight.flowRight(...flattenFuncs);
}
//#endregion
exports.flowRight = flowRight;
