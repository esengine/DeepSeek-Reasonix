import { iteratee } from "./iteratee.mjs";
//#region src/compat/util/over.ts
/**
* Creates a function that invokes given functions and returns their results as an array.
*
* @param iteratees - The iteratees to invoke.
* @returns Returns the new function.
*
* @example
* const func = over([Math.max, Math.min]);
* const func2 = over(Math.max, Math.min); // same as above
* func(1, 2, 3, 4);
* // => [4, 1]
* func2(1, 2, 3, 4);
* // => [4, 1]
*
* const func = over(['a', 'b']);
* func({ a: 1, b: 2 });
* // => [1, 2]
*
* const func = over([{ a: 1 }, { b: 2 }]);
* func({ a: 1, b: 2 });
* // => [true, true]
*
* const func = over([['a', 1], ['b', 2]]);
* func({ a: 1, b: 2 });
* // => [true, true]
*/
function over(...iteratees) {
	if (iteratees.length === 1 && Array.isArray(iteratees[0])) iteratees = iteratees[0];
	const funcs = iteratees.map((item) => iteratee(item));
	return function(...args) {
		return funcs.map((func) => func.apply(this, args));
	};
}
//#endregion
export { over };
