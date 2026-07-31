//#region src/compat/util/toPath.d.ts
/**
 * Converts a deep key string into an array of path segments.
 *
 * This function takes a string representing a deep key (e.g., 'a.b.c' or 'a[b][c]') and breaks it down into an array of strings, each representing a segment of the path.
 *
 * @param deepKey - The deep key string to convert.
 * @returns An array of strings, each representing a segment of the path.
 *
 * Examples:
 *
 * toPath('a.b.c') // Returns ['a', 'b', 'c']
 * toPath('a[b][c]') // Returns ['a', 'b', 'c']
 * toPath('.a.b.c') // Returns ['', 'a', 'b', 'c']
 * toPath('a["b.c"].d') // Returns ['a', 'b.c', 'd']
 * toPath('') // Returns []
 * toPath('.a[b].c.d[e]["f.g"].h') // Returns ['', 'a', 'b', 'c', 'd', 'e', 'f.g', 'h']
 */
declare function toPath(deepKey: any): string[];
//#endregion
export { toPath };