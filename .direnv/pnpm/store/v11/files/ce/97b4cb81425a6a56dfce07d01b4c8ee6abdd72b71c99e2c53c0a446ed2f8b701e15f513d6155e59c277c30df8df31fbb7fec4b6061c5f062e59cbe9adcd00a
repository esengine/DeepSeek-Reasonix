import { Equals } from "./Equals.js";

//#region src/compat/_internal/IsWritable.d.ts
type IsWritable<T> = Equals<{ [K in keyof T]: T[K] }, { -readonly [K in keyof T]: T[K] }>;
//#endregion
export { IsWritable };