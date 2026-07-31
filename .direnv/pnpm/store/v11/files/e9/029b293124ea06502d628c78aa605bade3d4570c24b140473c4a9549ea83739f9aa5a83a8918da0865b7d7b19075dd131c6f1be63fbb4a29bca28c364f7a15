//#region src/compat/_internal/isPrototype.ts
function isPrototype(value) {
	const constructor = value?.constructor;
	return value === (typeof constructor === "function" ? constructor.prototype : Object.prototype);
}
//#endregion
exports.isPrototype = isPrototype;
