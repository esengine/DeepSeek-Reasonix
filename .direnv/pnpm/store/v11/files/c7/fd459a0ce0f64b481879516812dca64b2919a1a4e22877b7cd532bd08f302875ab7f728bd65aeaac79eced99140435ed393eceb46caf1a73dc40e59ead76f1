const require_identity = require("../../function/identity.js");
const require_property = require("../object/property.js");
const require_matches = require("../predicate/matches.js");
const require_matchesProperty = require("../predicate/matchesProperty.js");
//#region src/compat/util/iteratee.ts
/**
* Creates a function that returns a value from an element in a collection.
*
* You can call `iteratee` with the following types of arguments:
*
* - **Function**: Returns the function as-is, which will be called with the element from the collection.
* - **Property name**: Returns the value of the specified property from the element.
* - **Property-value pair**: Returns a boolean indicating whether the element's property matches the given value.
* - **Partial object**: Returns a boolean indicating whether the element matches the properties of the partial object.
*
* If you don't provide any arguments or pass `null`, this function will return a function that simply returns its input unchanged.
*
* @param value - The value to convert to an iteratee.
* @returns Returns the new iteratee function.
* @example
* const func = iteratee();
* [{ a: 1 }, { a: 2 }, { a: 3 }].map(func) // => [{ a: 1 }, { a: 2 }, { a: 3 }]
*
* const func = iteratee((object) => object.a);
* [{ a: 1 }, { a: 2 }, { a: 3 }].map(func) // => [1, 2, 3]
*
* const func = iteratee('a');
* [{ a: 1 }, { a: 2 }, { a: 3 }].map(func) // => [1, 2, 3]
*
* const func = iteratee({ a: 1 });
* [{ a: 1 }, { a: 2 }, { a: 3 }].find(func) // => { a: 1 }
*
* const func = iteratee(['a', 1]);
* [{ a: 1 }, { a: 2 }, { a: 3 }].find(func) // => { a: 1 }
*/
function iteratee(value) {
	if (value == null) return require_identity.identity;
	switch (typeof value) {
		case "function": return value;
		case "object":
			if (Array.isArray(value) && value.length === 2) return require_matchesProperty.matchesProperty(value[0], value[1]);
			return require_matches.matches(value);
		case "string":
		case "symbol":
		case "number": return require_property.property(value);
	}
}
//#endregion
exports.iteratee = iteratee;
