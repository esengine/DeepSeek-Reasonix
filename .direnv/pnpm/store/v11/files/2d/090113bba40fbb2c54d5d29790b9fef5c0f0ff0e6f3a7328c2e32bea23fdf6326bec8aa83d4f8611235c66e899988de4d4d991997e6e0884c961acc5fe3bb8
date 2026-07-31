//#region src/string/words.d.ts
/**
 * Regular expression pattern to split strings into words for various case conversions
 *
 * This pattern matches sequences of characters in a string, considering the following cases:
 * - Sequences of two or more uppercase letters followed by an uppercase letter and lowercase letters or digits (for acronyms)
 * - Sequences of one uppercase letter optionally followed by lowercase letters and digits
 * - Single uppercase letters
 * - Sequences of digits
 * - Emojis and other Unicode characters
 *
 * The resulting match can be used to convert camelCase, snake_case, kebab-case, and other mixed formats into
 * a consistent format like snake case. It also supports emojis and other Unicode characters.
 *
 * @example
 * const matches = 'camelCaseHTTPRequest🚀'.match(CASE_SPLIT_PATTERN);
 * // matches: ['camel', 'Case', 'HTTP', 'Request', '🚀']
 */
declare const CASE_SPLIT_PATTERN: RegExp;
/**
 * Splits `string` into an array of its words, treating spaces and punctuation marks as separators.
 *
 * @param str The string to inspect.
 * @param [pattern] The pattern to match words.
 * @returns Returns the words of `string`.
 *
 * @example
 * words('fred, barney, & pebbles');
 * // => ['fred', 'barney', 'pebbles']
 *
 * words('camelCaseHTTPRequest🚀');
 * // => ['camel', 'Case', 'HTTP', 'Request', '🚀']
 *
 * words('Lunedì 18 Set')
 * // => ['Lunedì', '18', 'Set']
 */
declare function words(str: string): string[];
//#endregion
export { words };