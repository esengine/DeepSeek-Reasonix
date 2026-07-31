import { IsMatchWithCustomizer } from "../_internal/IsMatchWithCustomizer.mjs";

//#region src/compat/predicate/isMatchWith.d.ts
/**
 * Performs a deep comparison between a target value and a source pattern to determine if they match,
 * using a custom comparison function for fine-grained control over the matching logic.
 *
 * @param target - The value to be tested for matching
 * @param source - The pattern/template to match against
 * @param compare - Custom comparison function for fine-grained control
 * @returns `true` if the target matches the source pattern, `false` otherwise
 *
 * @example
 * // Basic matching with custom comparator
 * const caseInsensitiveCompare = (objVal, srcVal) => {
 *   if (typeof objVal === 'string' && typeof srcVal === 'string') {
 *     return objVal.toLowerCase() === srcVal.toLowerCase();
 *   }
 *   return undefined;
 * };
 *
 * isMatchWith(
 *   { name: 'JOHN', age: 30 },
 *   { name: 'john' },
 *   caseInsensitiveCompare
 * ); // true
 */
declare function isMatchWith(target: object, source: object, compare: IsMatchWithCustomizer): boolean;
declare function isSetMatch(target: unknown, source: Set<any>, compare: (objValue: any, srcValue: any, key: PropertyKey, object: any, source: any, stack?: Map<any, any>) => boolean | undefined, stack?: Map<any, any>): boolean;
//#endregion
export { isMatchWith };