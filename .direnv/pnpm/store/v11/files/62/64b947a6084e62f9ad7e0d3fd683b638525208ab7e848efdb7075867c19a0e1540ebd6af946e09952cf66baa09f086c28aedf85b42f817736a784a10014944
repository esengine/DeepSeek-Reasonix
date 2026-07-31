/**
 * Small module for atom-group constants and type guard.  Kept separate from
 * `symbols.ts` so that consumers (notably `contrib/render-a11y-string`) can
 * pull in `isAtom` without dragging in the ~870-line symbol tables.
 */

// Some of these have a "-token" suffix since these are also used as `ParseNode`
// types for raw text tokens, and we want to avoid conflicts with higher-level
// `ParseNode` types. These `ParseNode`s are constructed within `Parser` by
// looking up the `symbols` map.
export const ATOMS = {
    "bin": 1,
    "close": 1,
    "inner": 1,
    "open": 1,
    "punct": 1,
    "rel": 1,
};
export const NON_ATOMS = {
    "accent-token": 1,
    "mathord": 1,
    "op-token": 1,
    "spacing": 1,
    "textord": 1,
};

export type Atom = keyof typeof ATOMS;
export type NonAtom = keyof typeof NON_ATOMS;
export type Group = Atom | NonAtom;

export function isAtom(value: string): value is Atom {
    return value in ATOMS;
}
