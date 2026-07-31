const require_random = require("./random.js");
//#region src/math/randomInt.ts
/**
* Generates a random integer between minimum (inclusive) and maximum (exclusive).
*
* If only one argument is provided, a number between `0` and the given number is returned.
*
* @param minimum - The lower bound (inclusive).
* @param maximum - The upper bound (exclusive).
* @returns A random integer between minimum (inclusive) and maximum (exclusive).
* @throws {Error} Throws an error if `maximum` is not greater than `minimum`.
*
* @example
* const result = randomInt(0, 5); // result will be a random integer between 0 (inclusive) and 5 (exclusive)
* const result2 = randomInt(5, 0); // This will throw an error
*/
function randomInt(minimum, maximum) {
	return Math.floor(require_random.random(minimum, maximum));
}
//#endregion
exports.randomInt = randomInt;
