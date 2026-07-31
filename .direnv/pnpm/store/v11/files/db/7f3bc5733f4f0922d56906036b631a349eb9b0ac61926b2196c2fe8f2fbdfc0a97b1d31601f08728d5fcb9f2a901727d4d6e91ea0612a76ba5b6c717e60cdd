//#region src/compat/string/truncate.d.ts
type TruncateOptions = {
  length?: number;
  separator?: string | RegExp;
  omission?: string;
};
/**
 * This regex might more completely detect unicode, but it is slower and this project
 * desires to mimic the behavior of lodash.
 */
/**
 * Truncates `string` if it's longer than the given maximum string length.
 * The last characters of the truncated string are replaced with the omission
 * string which defaults to "...".
 *
 * @param [string=''] The string to truncate.
 * @param [options={}] The options object.
 * @param [options.length=30] The maximum string length.
 * @param [options.omission='...'] The string to indicate text is omitted.
 * @param [options.separator] The separator pattern to truncate to.
 *
 * @example
 * const test = 'hi-diddly-ho there, neighborino';
 * const truncatedStr1 = truncate(test) // returns 'hi-diddly-ho there, neighbo...'
 * const truncatedStr2 = truncate(test, { length: 24, separator: ' ' }) // returns 'hi-diddly-ho there,...'
 * const truncatedStr3 = truncate(test, { length: 24, separator: /,? +/ }) // returns 'hi-diddly-ho there...'
 * const truncatedStr4 = truncate(test, { omission: ' [...]' }) // returns 'hi-diddly-ho there, neig [...]'
 * const truncatedStr5 = truncate('ABC', { length: 3 }) // returns 'ABC'
 * const truncatedStr6 = truncate('ABC', { length: 2 }) // returns '...'
 * const truncatedStr7 = truncate('¥§✈✉🤓', { length: 5 }) // returns '¥§✈✉🤓'
 * const truncatedStr8 = truncate('¥§✈✉🤓', { length: 4, omission: '…' }) // returns '¥§✈…'
 */
declare function truncate(string?: string, options?: TruncateOptions): string;
//#endregion
export { truncate };