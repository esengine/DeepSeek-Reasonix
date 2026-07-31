//#region src/function/spread.ts
/**
* Creates a new function that spreads elements of an array argument into individual arguments
* for the original function.
*
* @template F - A function type with any number of parameters and any return type.
* @param func - The function to be transformed. It can be any function with any number of arguments.
* @returns A new function that takes an array of arguments and returns the result of calling the original function with those arguments.
*
* @example
* function add(a, b) {
*   return a + b;
* }
*
* const spreadAdd = spread(add);
* console.log(spreadAdd([1, 2])); // Output: 3
*/
function spread(func) {
	return function(argsArr) {
		return func.apply(this, argsArr);
	};
}
//#endregion
export { spread };
