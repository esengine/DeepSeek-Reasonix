//#region src/compat/_internal/TupleIterator.d.ts
type TupleIterator<T extends readonly unknown[], TResult> = (value: T[number], index: T extends `${infer N extends number}` ? N : never, collection: T) => TResult;
//#endregion
export { TupleIterator };