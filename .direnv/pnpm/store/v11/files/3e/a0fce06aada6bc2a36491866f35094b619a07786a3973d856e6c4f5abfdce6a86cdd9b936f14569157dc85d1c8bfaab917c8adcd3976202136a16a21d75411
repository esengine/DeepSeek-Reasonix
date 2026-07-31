const require_flatten = require("../../array/flatten.js");
const require_isIterateeCall = require("../_internal/isIterateeCall.js");
const require_orderBy = require("./orderBy.js");
//#region src/compat/array/sortBy.ts
function sortBy(collection, ...criteria) {
	const length = criteria.length;
	if (length > 1 && require_isIterateeCall.isIterateeCall(collection, criteria[0], criteria[1])) criteria = [];
	else if (length > 2 && require_isIterateeCall.isIterateeCall(criteria[0], criteria[1], criteria[2])) criteria = [criteria[0]];
	return require_orderBy.orderBy(collection, require_flatten.flatten(criteria), ["asc"]);
}
//#endregion
exports.sortBy = sortBy;
