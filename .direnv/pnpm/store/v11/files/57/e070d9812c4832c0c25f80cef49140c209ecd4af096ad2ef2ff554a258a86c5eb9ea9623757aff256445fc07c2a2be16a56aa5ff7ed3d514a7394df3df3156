//#region src/fp/array/unzip.d.ts
type Unzip<K extends unknown[]> = { [I in keyof K]: Array<K[I]> };
/**
 * Creates a function that unzips grouped tuples into arrays by position.
 *
 * The returned function is the data-last form of the main {@link unzip} utility.
 * Use it with {@link pipe}.
 *
 * @template T - The tuple type inside the zipped array.
 * @returns A function that maps zipped tuples to arrays grouped by tuple position.
 *
 * @example
 * import { pipe, unzip } from 'es-toolkit/fp';
 *
 * pipe([[1, 'a'], [2, 'b']] as Array<[number, string]>, unzip());
 * // => [[1, 2], ['a', 'b']]
 */
declare function unzip<T extends unknown[]>(): (zipped: ReadonlyArray<[...T]>) => Unzip<T>;
//#endregion
export { unzip };