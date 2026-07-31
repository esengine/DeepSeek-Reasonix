//#region src/compat/function/attempt.ts
/**
* Attempts to execute a function with the provided arguments.
* If the function throws an error, it catches the error and returns it.
* If the caught error is not an instance of Error, it wraps it in a new Error.
*
* @param func - The function to be executed.
* @param args - The arguments to pass to the function.
* @returns The return value of the function if successful, or an Error if an exception is thrown.
*
* @template R - The type of the function return value.
*
* @example
* // Example 1: Successful execution
* const result = attempt((x, y) => x + y, 2, 3);
* console.log(result); // Output: 5
*
* @example
* // Example 2: Function throws an error
* const errorResult = attempt(() => {
*   throw new Error("Something went wrong");
* });
* console.log(errorResult); // Output: Error: Something went wrong
*
* @example
* // Example 3: Non-Error thrown
* const nonErrorResult = attempt(() => {
*   throw "This is a string error";
* });
* console.log(nonErrorResult); // Output: Error: This is a string error
*/
function attempt(func, ...args) {
	try {
		return func(...args);
	} catch (e) {
		return e instanceof Error ? e : new Error(e);
	}
}
//#endregion
export { attempt };
