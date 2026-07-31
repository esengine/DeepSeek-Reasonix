//#region src/function/unary.d.ts
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
declare function unary<F extends (...args: any[]) => any>(func: F): (...args: any[]) => ReturnType<F>;
//#endregion
export { unary };