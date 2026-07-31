import { MutableList } from "./MutableList.js";
import { IsWritable } from "./IsWritable.js";

//#region src/compat/_internal/RejectReadonly.d.ts
type RejectReadonly<T extends MutableList<unknown>> = IsWritable<T> extends true ? T : never;
//#endregion
export { RejectReadonly };