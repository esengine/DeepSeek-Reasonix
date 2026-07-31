import { MutableList } from "./MutableList.mjs";
import { IsWritable } from "./IsWritable.mjs";

//#region src/compat/_internal/RejectReadonly.d.ts
type RejectReadonly<T extends MutableList<unknown>> = IsWritable<T> extends true ? T : never;
//#endregion
export { RejectReadonly };