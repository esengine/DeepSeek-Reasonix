import { iteratee } from "./iteratee.mjs";
//#region src/compat/util/overEvery.ts
/**
* Creates a function that checks if all of the given predicates return truthy for the provided values.
*
* This function takes multiple predicates, which can either be individual predicate functions or arrays of predicates,
* and returns a new function that checks if all of the predicates return truthy when called with the provided values.
*
* @template T - The type of the values to be checked.
*
* @param predicates -
*   A list of predicates or arrays of predicates. Each predicate is a function that takes one or more values of
*   type `T` and returns a boolean indicating whether the condition is satisfied for those values.
*
* @returns A function that takes a list of values and returns `true` if all of the
*   predicates return truthy for the provided values, and `false` otherwise.
*
* @example
* const func = overEvery(
*   (value) => typeof value === 'string',
*   (value) => value.length > 3
* );
*
* func("hello"); // true
* func("hi"); // false
* func(42); // false
*
* @example
* const func = overEvery([
*   (value) => value.a > 0,
*   (value) => value.b > 0
* ]);
*
* func({ a: 1, b: 2 }); // true
* func({ a: 0, b: 2 }); // false
*
* @example
* const func = overEvery(
*   (a, b) => typeof a === 'string' && typeof b === 'string',
*   (a, b) => a.length > 3 && b.length > 3
* );
*
* func("hello", "world"); // true
* func("hi", "world"); // false
* func(1, 10); // false
*/
function overEvery(...predicates) {
	return function(...values) {
		for (let i = 0; i < predicates.length; ++i) {
			const predicate = predicates[i];
			if (!Array.isArray(predicate)) {
				if (!iteratee(predicate).apply(this, values)) return false;
				continue;
			}
			for (let j = 0; j < predicate.length; ++j) if (!iteratee(predicate[j]).apply(this, values)) return false;
		}
		return true;
	};
}
//#endregion
export { overEvery };
