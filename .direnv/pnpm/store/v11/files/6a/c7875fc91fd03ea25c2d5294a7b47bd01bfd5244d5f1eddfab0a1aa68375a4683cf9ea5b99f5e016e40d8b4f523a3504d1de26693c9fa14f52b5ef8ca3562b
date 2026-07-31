import { random as random$1 } from "../../math/random.mjs";
import { randomInt } from "../../math/randomInt.mjs";
import { clamp } from "./clamp.mjs";
//#region src/compat/math/random.ts
/**
* Generate a random number within the given range.
*
* @param minimum - The lower bound (inclusive).
* @param maximum - The upper bound (exclusive).
* @returns A random number between minimum (inclusive) and maximum (exclusive). The number can be an integer or a decimal.
* @throws {Error} Throws an error if `maximum` is not greater than `minimum`.
*
* @example
* const result1 = random(0, 5); // Returns a random number between 0 and 5.
* const result2 = random(5, 0); // If the minimum is greater than the maximum, an error is thrown.
* const result3 = random(5, 5); // If the minimum is equal to the maximum, an error is thrown.
*/
function random(...args) {
	let minimum = 0;
	let maximum = 1;
	let floating = false;
	switch (args.length) {
		case 1:
			if (typeof args[0] === "boolean") floating = args[0];
			else maximum = args[0];
			break;
		case 2: if (typeof args[1] === "boolean") {
			maximum = args[0];
			floating = args[1];
			break;
		} else {
			minimum = args[0];
			maximum = args[1];
		}
		case 3: if (typeof args[2] === "object" && args[2] != null && args[2][args[1]] === args[0]) {
			minimum = 0;
			maximum = args[0];
			floating = false;
		} else {
			minimum = args[0];
			maximum = args[1];
			floating = args[2];
		}
	}
	if (typeof minimum !== "number") minimum = Number(minimum);
	if (typeof maximum !== "number") maximum = Number(maximum);
	if (!minimum) minimum = 0;
	if (!maximum) maximum = 0;
	if (minimum > maximum) [minimum, maximum] = [maximum, minimum];
	if (!floating && (!Number.isInteger(minimum) || !Number.isInteger(maximum))) floating = true;
	minimum = clamp(minimum, -Number.MAX_SAFE_INTEGER, Number.MAX_SAFE_INTEGER);
	maximum = clamp(maximum, -Number.MAX_SAFE_INTEGER, Number.MAX_SAFE_INTEGER);
	if (minimum === maximum) return minimum;
	if (floating) return random$1(minimum, maximum);
	else return randomInt(minimum, maximum + 1);
}
//#endregion
export { random };
