//#region src/function/after.ts
/**
* Creates a function that only executes starting from the `n`-th call.
* The provided function will be invoked starting from the `n`-th call.
*
* This is particularly useful for scenarios involving events or asynchronous operations
* where an action should occur only after a certain number of invocations.
*
* @template F - The type of the function to be invoked.
* @param n - The number of calls required for `func` to execute.
* @param func - The function to be invoked.
* @returns A new function that:
* - Tracks the number of calls.
* - Invokes `func` starting from the `n`-th call.
* - Returns `undefined` if fewer than `n` calls have been made.
* @throws {Error} - Throws an error if `n` is negative.
* @example
*
* const afterFn = after(3, () => {
*  console.log("called")
* });
*
* // Will not log anything.
* afterFn()
* // Will not log anything.
* afterFn()
* // Will log 'called'.
* afterFn()
*/
function after(n, func) {
	if (!Number.isInteger(n) || n < 0) throw new Error(`n must be a non-negative integer.`);
	let counter = 0;
	return (...args) => {
		if (++counter >= n) return func(...args);
	};
}
//#endregion
exports.after = after;
