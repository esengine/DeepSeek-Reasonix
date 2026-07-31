//#region src/compat/predicate/matches.d.ts
/**
 * Creates a function that performs a deep comparison between a given target and the source object.
 *
 * @template T
 * @param source - The source object to create the matcher from.
 * @returns Returns a function that takes a target object and returns `true` if the target matches the source, otherwise `false`.
 *
 * @example
 * const matcher = matches({ a: 1, b: 2 });
 * matcher({ a: 1, b: 2, c: 3 }); // true
 * matcher({ a: 1, c: 3 }); // false
 */
declare function matches<T>(source: T): (value: any) => boolean;
/**
 * Creates a function that performs a deep comparison between a given target and the source object.
 *
 * @template T
 * @template V
 * @param source - The source object to create the matcher from.
 * @returns Returns a function that takes a target object and returns `true` if the target matches the source, otherwise `false`.
 *
 * @example
 * const matcher = matches<{ a: number }, { a: number; b?: number }>({ a: 1 });
 * matcher({ a: 1, b: 2 }); // true
 * matcher({ a: 2 }); // false
 */
declare function matches<T, V>(source: T): (value: V) => boolean;
//#endregion
export { matches };