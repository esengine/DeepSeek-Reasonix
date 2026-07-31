import { ObjectIterator } from "./ObjectIterator.mjs";
import { IterateeShorthand } from "./IterateeShorthand.mjs";

//#region src/compat/_internal/ObjectIteratee.d.ts
type ObjectIteratee<TObject> = ObjectIterator<TObject, unknown> | IterateeShorthand<TObject[keyof TObject]>;
type ObjectIterateeCustom<TObject, TResult> = ObjectIterator<TObject, TResult> | IterateeShorthand<TObject[keyof TObject]>;
//#endregion
export { ObjectIteratee, ObjectIterateeCustom };