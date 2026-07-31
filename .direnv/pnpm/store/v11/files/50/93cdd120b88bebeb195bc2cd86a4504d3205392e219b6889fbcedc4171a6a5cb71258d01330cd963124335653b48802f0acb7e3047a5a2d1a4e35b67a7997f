//#region src/compat/string/startsWith.ts
/**
* Checks if a string contains another string at the beginning of the string.
*
* Checks if one string startsWith another string. Optional position parameter to start searching from a certain index.
*
* @param str - The string that might contain the target string.
* @param target - The string to search for.
* @param position - An optional offset to start searching in the str string
* @returns True if the str string starts with the target string.
*
* @example
* const isPrefix = startsWith('fooBar', 'foo') // returns true
* const isPrefix = startsWith('fooBar', 'bar') // returns false
* const isPrefix = startsWith('fooBar', 'abc') // returns false
* const isPrefix = startsWith('fooBar', 'Bar', 2) // returns true
* const isPrefix = startsWith('fooBar', 'Bar', 5) // returns false
*/
function startsWith(str, target, position) {
	if (str == null || target == null) return false;
	if (position == null) position = 0;
	return str.startsWith(target, position);
}
//#endregion
export { startsWith };
