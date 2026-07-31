//#region src/function/curryRight.ts
/**
* Curries a function, allowing it to be called with a single argument at a time and returning a new function that takes the next argument.
* This process continues until all arguments have been provided, at which point the original function is called with all accumulated arguments.
*
* Unlike `curry`, this function curries the function from right to left.
*
* @param func - The function to curry.
* @returns A curried function.
*
* @example
* function sum(a: number, b: number, c: number) {
*   return a + b + c;
* }
*
* const curriedSum = curryRight(sum);
*
* // The parameter `c` should be given the value `10`.
* const add10 = curriedSum(10);
*
* // The parameter `b` should be given the value `15`.
* const add25 = add10(15);
*
* // The parameter `a` should be given the value `5`. The function 'sum' has received all its arguments and will now return a value.
* const result = add25(5); // 30
*/
function curryRight(func) {
	if (func.length === 0 || func.length === 1) return func;
	return function(arg) {
		return makeCurryRight(func, func.length, [arg]);
	};
}
function makeCurryRight(origin, argsLength, args) {
	if (args.length === argsLength) return origin(...args);
	else {
		const next = function(arg) {
			return makeCurryRight(origin, argsLength, [arg, ...args]);
		};
		return next;
	}
}
//#endregion
exports.curryRight = curryRight;
