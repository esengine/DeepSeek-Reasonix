const require_identity = require("../../function/identity.js");
const require_iteratee = require("../util/iteratee.js");
//#region src/compat/function/overArgs.ts
/**
* Creates a function that invokes `func` with its arguments transformed by corresponding transform functions.
*
* Transform functions can be:
* - Functions that accept and return a value
* - Property names (strings) to get a property value from each argument
* - Objects to check if arguments match the object properties
* - Arrays of [property, value] to check if argument properties match values
*
* If a transform is nullish, the identity function is used instead.
* Only transforms arguments up to the number of transform functions provided.
*
* @param func - The function to wrap
* @param transforms - The functions to transform arguments. Each transform can be:
*   - A function that accepts and returns a value
*   - A string to get a property value (e.g. 'name' gets the name property)
*   - An object to check if arguments match its properties
*   - An array of [property, value] to check property matches
* @returns A new function that transforms arguments before passing them to func
* @throws {TypeError} If func is not a function.
* @example
* ```ts
* function doubled(n: number) {
*   return n * 2;
* }
*
* function square(n: number) {
*   return n * n;
* }
*
* const func = overArgs((x, y) => [x, y], [doubled, square]);
*
* func(5, 3);
* // => [10, 9]
*
* // With property shorthand
* const user = { name: 'John', age: 30 };
* const getUserInfo = overArgs(
*   (name, age) => `${name} is ${age} years old`,
*   ['name', 'age']
* );
* getUserInfo(user, user);
* // => "John is 30 years old"
* ```
*/
function overArgs(func, ..._transforms) {
	if (typeof func !== "function") throw new TypeError("Expected a function");
	const transforms = _transforms.flat();
	return function(...args) {
		const length = Math.min(args.length, transforms.length);
		const transformedArgs = [...args];
		for (let i = 0; i < length; i++) transformedArgs[i] = require_iteratee.iteratee(transforms[i] ?? require_identity.identity).call(this, args[i]);
		return func.apply(this, transformedArgs);
	};
}
//#endregion
exports.overArgs = overArgs;
