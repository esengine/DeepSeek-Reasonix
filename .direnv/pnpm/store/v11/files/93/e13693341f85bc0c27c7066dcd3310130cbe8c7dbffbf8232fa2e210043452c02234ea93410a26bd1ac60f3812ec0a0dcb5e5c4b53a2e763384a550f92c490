const require_isEqualsSameValueZero = require("../../_internal/isEqualsSameValueZero.js");
require("../util/eq.js");
//#region src/compat/_internal/assignValue.ts
const assignValue = (object, key, value) => {
	const objValue = object[key];
	if (!(Object.hasOwn(object, key) && require_isEqualsSameValueZero.isEqualsSameValueZero(objValue, value)) || value === void 0 && !(key in object)) object[key] = value;
};
//#endregion
exports.assignValue = assignValue;
