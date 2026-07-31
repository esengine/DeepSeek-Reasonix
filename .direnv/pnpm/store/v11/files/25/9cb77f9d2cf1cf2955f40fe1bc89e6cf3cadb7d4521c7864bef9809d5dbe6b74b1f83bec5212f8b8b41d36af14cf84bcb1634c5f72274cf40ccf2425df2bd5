//#region src/string/deburr.ts
const deburrMap = new Map([
	["Æ", "Ae"],
	["Ð", "D"],
	["Ø", "O"],
	["Þ", "Th"],
	["ß", "ss"],
	["æ", "ae"],
	["ð", "d"],
	["ø", "o"],
	["þ", "th"],
	["Đ", "D"],
	["đ", "d"],
	["Ħ", "H"],
	["ħ", "h"],
	["ı", "i"],
	["Ĳ", "IJ"],
	["ĳ", "ij"],
	["ĸ", "k"],
	["Ŀ", "L"],
	["ŀ", "l"],
	["Ł", "L"],
	["ł", "l"],
	["ŉ", "'n"],
	["Ŋ", "N"],
	["ŋ", "n"],
	["Œ", "Oe"],
	["œ", "oe"],
	["Ŧ", "T"],
	["ŧ", "t"],
	["ſ", "s"]
]);
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
	str = str.normalize("NFD");
	let result = "";
	for (let i = 0; i < str.length; i++) {
		const char = str[i];
		if (char >= "̀" && char <= "ͯ" || char >= "︠" && char <= "︣") continue;
		result += deburrMap.get(char) ?? char;
	}
	return result;
}
//#endregion
exports.deburr = deburr;
