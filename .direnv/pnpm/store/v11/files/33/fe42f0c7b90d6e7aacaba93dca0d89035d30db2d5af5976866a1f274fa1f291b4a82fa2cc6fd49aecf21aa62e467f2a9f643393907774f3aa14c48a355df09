//#region src/compat/_internal/EmptyObjectOf.d.ts
type EmptyObject<T> = { [K in keyof T]?: never };
type EmptyObjectOf<T> = EmptyObject<T> extends T ? EmptyObject<T> : never;
//#endregion
export { EmptyObjectOf };