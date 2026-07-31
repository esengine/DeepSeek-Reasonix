//#region src/function/ary.ts
/**
* Creates a function that invokes func, with up to n arguments, ignoring any additional arguments.
*
* @template F - The type of the function.
* @param func - The function to cap arguments for.
* @param n - The arity cap.
* @returns Returns the new capped function.
*
* @example
* function fn(a: number, b: number, c: number) {
*   return Array.from(arguments);
* }
*
* ary(fn, 0)(1, 2, 3) // []
* ary(fn, 1)(1, 2, 3) // [1]
* ary(fn, 2)(1, 2, 3) // [1, 2]
* ary(fn, 3)(1, 2, 3) // [1, 2, 3]
*/
function ary(func, n) {
	return function(...args) {
		return func.apply(this, args.slice(0, n));
	};
}
//#endregion
exports.ary = ary;
