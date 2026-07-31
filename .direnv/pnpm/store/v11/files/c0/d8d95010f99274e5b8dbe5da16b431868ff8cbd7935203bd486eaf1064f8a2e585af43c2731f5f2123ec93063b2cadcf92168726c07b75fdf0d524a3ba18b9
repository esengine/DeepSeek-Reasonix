//#region src/compat/_internal/PartialShallow.d.ts
type PartialShallow<T> = { [P in keyof T]?: T[P] extends object ? object : T[P] };
//#endregion
export { PartialShallow };