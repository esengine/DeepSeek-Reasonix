//#region src/compat/string/capitalize.d.ts
/**
 * Converts the first character of string to upper case and the remaining to lower case.
 *
 * @param string - The string to capitalize.
 * @returns The capitalized string.
 *
 * @example
 * const convertedStr1 = capitalize('fred') // returns 'Fred'
 * const convertedStr2 = capitalize('FRED') // returns 'Fred'
 * const convertedStr3 = capitalize('') // returns ''
 */
declare function capitalize<T extends string>(str?: T): string extends T ? string : Capitalize<Lowercase<T>>;
//#endregion
export { capitalize };