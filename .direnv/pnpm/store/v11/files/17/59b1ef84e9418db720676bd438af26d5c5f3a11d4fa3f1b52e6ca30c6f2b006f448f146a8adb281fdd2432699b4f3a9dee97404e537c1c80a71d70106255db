const require_iteratee = require("./iteratee.js");
//#region src/compat/util/overSome.ts
/**
* Creates a function that checks if any of the given predicates return truthy for the provided values.
*
* This function takes multiple predicates, which can either be individual predicate functions or arrays of predicates,
* and returns a new function that checks if any of the predicates return truthy when called with the provided values.
*
* @template T - The type of the values to be checked.
*
* @param predicates -
*   A list of predicates or arrays of predicates. Each predicate is a function that takes one or more values of
*   type `T` and returns a boolean indicating whether the condition is satisfied for those values.
*
* @returns A function that takes a list of values and returns `true` if any of the
*   predicates return truthy for the provided values, and `false` otherwise.
*
* @example
* const func = overSome(
*   (value) => typeof value === 'string',
*   (value) => typeof value === 'number',
*   (value) => typeof value === 'symbol'
* );
*
* func("hello"); // true
* func(42); // true
* func(Symbol()); // true
* func([]); // false
*
* @example
* const func = overSome([
*   (value) => value.a > 0,
*   (value) => value.b > 0
* ]);
*
* func({ a: 0, b: 2 }); // true
* func({ a: 0, b: 0 }); // false
*
* @example
* const func = overSome(
*   (a, b) => typeof a === 'string' && typeof b === 'string',
*   (a, b) => a > 0 && b > 0
* );
*
* func("hello", "world"); // true
* func(1, 10); // true
* func(0, 2); // false
*/
function overSome(...predicates) {
	return function(...values) {
		for (let i = 0; i < predicates.length; ++i) {
			const predicate = predicates[i];
			if (!Array.isArray(predicate)) {
				if (require_iteratee.iteratee(predicate).apply(this, values)) return true;
				continue;
			}
			for (let j = 0; j < predicate.length; ++j) if (require_iteratee.iteratee(predicate[j]).apply(this, values)) return true;
		}
		return false;
	};
}
//#endregion
exports.overSome = overSome;
