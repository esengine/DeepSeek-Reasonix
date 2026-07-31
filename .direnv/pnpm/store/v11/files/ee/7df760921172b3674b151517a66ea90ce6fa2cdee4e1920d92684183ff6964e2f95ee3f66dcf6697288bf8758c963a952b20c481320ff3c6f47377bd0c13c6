//#region src/function/flow.ts
/**
* Creates a new function that executes the given functions in sequence. The return value of the previous function is passed as an argument to the next function.
*
* The `this` context of the returned function is also passed to the functions provided as parameters.
*
* @param funcs The functions to invoke.
* @returns Returns the new composite function.
*
* @example
* const add = (x: number, y: number) => x + y;
* const square = (n: number) => n * n;
*
* const combined = flow(add, square);
* console.log(combined(1, 2)); // 9
*/
function flow(...funcs) {
	return function(...args) {
		let result = funcs.length ? funcs[0].apply(this, args) : args[0];
		for (let i = 1; i < funcs.length; i++) result = funcs[i].call(this, result);
		return result;
	};
}
//#endregion
export { flow };
