import { flatten } from "../array/flatten.mjs";
//#region src/compat/function/rearg.ts
/**
* Creates a function that invokes `func` with arguments arranged according to the specified `indices`
* where the argument value at the first index is provided as the first argument,
* the argument value at the second index is provided as the second argument, and so on.
*
* @param func The function to rearrange arguments for.
* @param indices The arranged argument indices.
* @returns Returns the new function.
*
* @example
* const greet = (greeting: string, name: string) => `${greeting}, ${name}!`;
* const rearrangedGreet = rearg(greet, 1, 0);
* console.log(rearrangedGreet('World', 'Hello')); // Output: "Hello, World!"
*/
function rearg(func, ...indices) {
	const flattenIndices = flatten(indices);
	return function(...args) {
		const reorderedArgs = flattenIndices.map((i) => args[i]).slice(0, args.length);
		for (let i = reorderedArgs.length; i < args.length; i++) reorderedArgs.push(args[i]);
		return func.apply(this, reorderedArgs);
	};
}
//#endregion
export { rearg };
