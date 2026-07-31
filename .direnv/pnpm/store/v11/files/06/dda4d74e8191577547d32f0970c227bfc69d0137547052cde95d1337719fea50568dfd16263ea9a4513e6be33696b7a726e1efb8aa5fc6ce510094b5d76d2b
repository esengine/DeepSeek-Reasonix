//#region src/function/curry.ts
/**
* Curries a function, allowing it to be called with a single argument at a time and returning a new function that takes the next argument.
* This process continues until all arguments have been provided, at which point the original function is called with all accumulated arguments.
*
* @param func - The function to curry.
* @returns A curried function that can be called with a single argument at a time.
*
* @example
* function sum(a: number, b: number, c: number) {
*   return a + b + c;
* }
*
* const curriedSum = curry(sum);
*
* // The parameter `a` should be given the value `10`.
* const add10 = curriedSum(10);
*
* // The parameter `b` should be given the value `15`.
* const add25 = add10(15);
*
* // The parameter `c` should be given the value `5`. The function 'sum' has received all its arguments and will now return a value.
* const result = add25(5);
*/
function curry(func) {
	if (func.length === 0 || func.length === 1) return func;
	return function(arg) {
		return makeCurry(func, func.length, [arg]);
	};
}
function makeCurry(origin, argsLength, args) {
	if (args.length === argsLength) return origin(...args);
	else {
		const next = function(arg) {
			return makeCurry(origin, argsLength, [...args, arg]);
		};
		return next;
	}
}
//#endregion
export { curry };
