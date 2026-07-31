//#region src/compat/_internal/ConformsPredicateObject.d.ts
type ConformsPredicateObject<T> = { [P in keyof T]: T[P] extends ((arg: infer A) => any) ? A : any };
//#endregion
export { ConformsPredicateObject };