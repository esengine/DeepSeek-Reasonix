//#region src/compat/predicate/isArrayLike.d.ts
/**
 * Checks if `value` is array-like. This overload is for compatibility with lodash type checking.
 *
 * @param t The value to check.
 * @returns Returns `true` if `value` is array-like, else `false`.
 */
declare function isArrayLike<T extends {
  __lodashAnyHack: any;
}>(t: T): boolean;
/**
 * Checks if `value` is array-like. Functions, null, and undefined are never array-like.
 *
 * @param value The value to check.
 * @returns Returns `false` for functions, null, and undefined.
 */
declare function isArrayLike(value: ((...args: any[]) => any) | null | undefined): value is never;
/**
 * Checks if `value` is array-like.
 *
 * @param value The value to check.
 * @returns} Returns `true` if `value` is array-like, else `false`.
 */
declare function isArrayLike(value: any): value is {
  length: number;
};
//#endregion
export { isArrayLike };