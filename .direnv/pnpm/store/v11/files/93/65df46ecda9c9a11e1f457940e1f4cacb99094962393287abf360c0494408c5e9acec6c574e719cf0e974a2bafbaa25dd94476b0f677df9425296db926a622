import { isEqualsSameValueZero } from "../../_internal/isEqualsSameValueZero.mjs";
import "../util/eq.mjs";
//#region src/compat/_internal/assignValue.ts
const assignValue = (object, key, value) => {
	const objValue = object[key];
	if (!(Object.hasOwn(object, key) && isEqualsSameValueZero(objValue, value)) || value === void 0 && !(key in object)) object[key] = value;
};
//#endregion
export { assignValue };
