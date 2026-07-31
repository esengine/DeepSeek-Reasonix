//#region src/string/trimStart.ts
/**
* Removes leading whitespace or specified characters from a string.
*
* If `chars` is a string, it should be a single character. To trim a string with multiple characters,
* provide an array instead.
*
* @param str - The string from which leading characters will be trimmed.
* @param chars - The character(s) to remove from the start of the string.
* @returns The resulting string after the specified leading character has been removed.
*
* @example
* const trimmedStr1 = trimStart('---hello', '-') // returns 'hello'
* const trimmedStr2 = trimStart('000123', '0') // returns '123'
* const trimmedStr3 = trimStart('abcabcabc', 'a') // returns 'bcabcabc'
* const trimmedStr4 = trimStart('xxxtrimmed', 'x') // returns 'trimmed'
*/
function trimStart(str, chars) {
	if (chars === void 0) return str.trimStart();
	let startIndex = 0;
	switch (typeof chars) {
		case "string":
			if (chars.length !== 1) throw new Error(`The 'chars' parameter should be a single character string.`);
			while (startIndex < str.length && str[startIndex] === chars) startIndex++;
			break;
		case "object": while (startIndex < str.length && chars.includes(str[startIndex])) startIndex++;
	}
	return str.substring(startIndex);
}
//#endregion
exports.trimStart = trimStart;
