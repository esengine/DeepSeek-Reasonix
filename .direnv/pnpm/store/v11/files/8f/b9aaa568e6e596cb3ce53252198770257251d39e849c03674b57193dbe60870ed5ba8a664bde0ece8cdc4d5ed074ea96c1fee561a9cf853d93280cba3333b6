//#region src/string/capitalize.d.ts
/**
 * Converts the first character of string to upper case and the remaining to lower case.
 *
 * @template T - Literal type of the string.
 * @param str - The string to be converted to uppercase.
 * @returns The capitalized string.
 *
 * @example
 * const result = capitalize('fred') // returns 'Fred'
 * const result2 = capitalize('FRED') // returns 'Fred'
 */
declare function capitalize<T extends string>(str: T): Capitalize<T>;
type Capitalize<T extends string> = T extends `${infer F}${infer R}` ? `${Uppercase<F>}${Lowercase<R>}` : T;
//#endregion
export { capitalize };