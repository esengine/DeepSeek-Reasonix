import { words } from "../../string/words.mjs";
import { deburr } from "./deburr.mjs";
import { normalizeForCase } from "../_internal/normalizeForCase.mjs";
//#region src/compat/string/startCase.ts
/**
* Converts the first character of each word in a string to uppercase and the remaining characters to lowercase.
*
* Start case is the naming convention in which each word is written with an initial capital letter.
* @param str - The string to convert.
* @returns The converted string.
*
* @example
* const result1 = startCase('hello world');  // result will be 'Hello World'
* const result2 = startCase('HELLO WORLD');  // result will be 'HELLO WORLD'
* const result3 = startCase('hello-world');  // result will be 'Hello World'
* const result4 = startCase('hello_world');  // result will be 'Hello World'
*/
function startCase(str) {
	const words$1 = words(normalizeForCase(deburr(str)).trim());
	let result = "";
	for (let i = 0; i < words$1.length; i++) {
		const word = words$1[i];
		if (result) result += " ";
		if (word === word.toUpperCase()) result += word;
		else result += word[0].toUpperCase() + word.slice(1).toLowerCase();
	}
	return result;
}
//#endregion
export { startCase };
