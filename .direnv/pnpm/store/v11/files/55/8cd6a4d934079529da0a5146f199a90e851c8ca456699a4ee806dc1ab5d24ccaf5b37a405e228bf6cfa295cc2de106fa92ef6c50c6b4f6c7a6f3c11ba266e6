/**
 * Find the first node in `parent` after another `node` or after an index,
 * that passes `test`.
 *
 * @param parent
 *   Parent node.
 * @param index
 *   Child node or index.
 * @param [test=undefined]
 *   Test for child to look for (optional).
 * @returns
 *   A child (matching `test`, if given) or `undefined`.
 */
export const findAfter: (<
  Kind extends import('unist').Parent,
  Check extends Test
>(
  parent: Kind,
  index: number | Child<Kind>,
  test: Check
) => Matches<Child<Kind>, Check> | undefined) &
  (<Kind_1 extends import('unist').Parent>(
    parent: Kind_1,
    index: number | Child<Kind_1>,
    test?: null | undefined
  ) => Child<Kind_1> | undefined)
export type UnistNode = import('unist').Node
export type UnistParent = import('unist').Parent
/**
 * Test from `unist-util-is`.
 *
 * Note: we have remove and add `undefined`, because otherwise when generating
 * automatic `.d.ts` files, TS tries to flatten paths from a local perspective,
 * which doesn’t work when publishing on npm.
 */
export type Test = Exclude<import('unist-util-is').Test, undefined> | undefined
/**
 * Get the value of a type guard `Fn`.
 */
export type Predicate<Fn, Fallback> = Fn extends (
  value: any
) => value is infer Thing
  ? Thing
  : Fallback
/**
 * Check whether a node matches a primitive check in the type system.
 */
export type MatchesOne<Value, Check> = Check extends null | undefined
  ? Value
  : Value extends {
      type: Check
    }
  ? Value
  : Value extends Check
  ? Value
  : Check extends Function
  ? Predicate<Check, Value> extends Value
    ? Predicate<Check, Value>
    : never
  : never
/**
 * Check whether a node matches a check in the type system.
 */
export type Matches<Value, Check> = Check extends Array<any>
  ? MatchesOne<Value, Check[keyof Check]>
  : MatchesOne<Value, Check>
/**
 * Collect nodes that can be parents of `Child`.
 */
export type Child<Kind extends import('unist').Node> = Kind extends {
  children: (infer Child_1)[]
}
  ? Child_1
  : never
