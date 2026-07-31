import { ary } from "./ary.mjs";
//#region src/function/unary.ts
/**
* Creates a function that accepts up to one argument, ignoring any additional arguments.
*
* @template F - The type of the function.
* @param func - The function to cap arguments for.
* @returns Returns the new capped function.
*
* @example
* function fn(a, b, c) {
*   console.log(arguments);
* }
*
* unary(fn)(1, 2, 3); // [Arguments] { '0': 1 }
*/
function unary(func) {
	return ary(func, 1);
}
//#endregion
export { unary };
