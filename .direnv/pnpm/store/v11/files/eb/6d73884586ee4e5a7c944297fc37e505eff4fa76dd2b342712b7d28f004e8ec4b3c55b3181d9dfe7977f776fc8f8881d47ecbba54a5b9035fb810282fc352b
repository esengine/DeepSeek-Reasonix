import { deburr as deburr$1 } from "../../string/deburr.mjs";
import { toString } from "../util/toString.mjs";
//#region src/compat/string/deburr.ts
/**
* Converts a string by replacing special characters and diacritical marks with their ASCII equivalents.
* For example, "Crème brûlée" becomes "Creme brulee".
*
* @param str - The input string to be deburred.
* @returns The deburred string with special characters replaced by their ASCII equivalents.
*
* @example
* // Basic usage:
* deburr('Æthelred') // returns 'Aethelred'
*
* @example
* // Handling diacritical marks:
* deburr('München') // returns 'Munchen'
*
* @example
* // Special characters:
* deburr('Crème brûlée') // returns 'Creme brulee'
*/
function deburr(str) {
	return deburr$1(toString(str));
}
//#endregion
export { deburr };
