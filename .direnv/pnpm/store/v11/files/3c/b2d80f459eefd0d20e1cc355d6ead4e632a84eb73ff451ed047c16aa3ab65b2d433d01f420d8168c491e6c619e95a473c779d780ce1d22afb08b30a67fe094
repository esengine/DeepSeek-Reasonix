import { flow } from "./flow.mjs";
//#region src/function/flowRight.ts
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
*
* const combined = flowRight(square, add);
* console.log(combined(1, 2)); // 9
*/
function flowRight(...funcs) {
	return flow(...funcs.reverse());
}
//#endregion
export { flowRight };
