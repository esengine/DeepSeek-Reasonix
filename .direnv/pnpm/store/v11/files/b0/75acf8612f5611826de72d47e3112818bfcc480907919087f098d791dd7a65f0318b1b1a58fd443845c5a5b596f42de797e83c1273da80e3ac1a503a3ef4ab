const require_toPath = require("../util/toPath.js");
const require_compareValues = require("../_internal/compareValues.js");
const require_isKey = require("../_internal/isKey.js");
//#region src/compat/array/orderBy.ts
/**
* Sorts an array of objects based on multiple properties and their corresponding order directions.
*
* This function takes an array of objects, an array of criteria to sort by, and an array of order directions.
* It returns the sorted array, ordering by each key according to its corresponding direction ('asc' for ascending or 'desc' for descending).
* If values for a key are equal, it moves to the next key to determine the order.
*
* @template T - The type of elements in the array.
* @param collection - The array of objects to be sorted.
* @param criteria - An array of criteria (property names or property paths or custom key functions) to sort by.
* @param orders - An array of order directions ('asc' for ascending or 'desc' for descending).
* @param [guard] Enables use as an iteratee for methods like `_.reduce`.
* @returns The sorted array.
*
* @example
* // Sort an array of objects by 'user' in ascending order and 'age' in descending order.
* const users = [
*   { user: 'fred', age: 48 },
*   { user: 'barney', age: 34 },
*   { user: 'fred', age: 40 },
*   { user: 'barney', age: 36 },
* ];
* const result = orderBy(users, ['user', (item) => item.age], ['asc', 'desc']);
* // result will be:
* // [
* //   { user: 'barney', age: 36 },
* //   { user: 'barney', age: 34 },
* //   { user: 'fred', age: 48 },
* //   { user: 'fred', age: 40 },
* // ]
*/
function orderBy(collection, criteria, orders, guard) {
	if (collection == null) return [];
	orders = guard ? void 0 : orders;
	if (!Array.isArray(collection)) collection = Object.values(collection);
	if (!Array.isArray(criteria)) criteria = criteria == null ? [null] : [criteria];
	if (criteria.length === 0) criteria = [null];
	if (!Array.isArray(orders)) orders = orders == null ? [] : [orders];
	orders = orders.map((order) => String(order));
	const getValueByNestedPath = (object, path) => {
		let target = object;
		for (let i = 0; i < path.length && target != null; ++i) target = target[path[i]];
		return target;
	};
	const getValueByCriterion = (criterion, object) => {
		if (object == null || criterion == null) return object;
		if (typeof criterion === "object" && "key" in criterion) {
			if (Object.hasOwn(object, criterion.key)) return object[criterion.key];
			return getValueByNestedPath(object, criterion.path);
		}
		if (typeof criterion === "function") return criterion(object);
		if (Array.isArray(criterion)) return getValueByNestedPath(object, criterion);
		if (typeof object === "object") return object[criterion];
		return object;
	};
	const preparedCriteria = criteria.map((criterion) => {
		if (Array.isArray(criterion) && criterion.length === 1) criterion = criterion[0];
		if (criterion == null || typeof criterion === "function" || Array.isArray(criterion) || require_isKey.isKey(criterion)) return criterion;
		return {
			key: criterion,
			path: require_toPath.toPath(criterion)
		};
	});
	return collection.map((item) => ({
		original: item,
		criteria: preparedCriteria.map((criterion) => getValueByCriterion(criterion, item))
	})).slice().sort((a, b) => {
		for (let i = 0; i < preparedCriteria.length; i++) {
			const comparedResult = require_compareValues.compareValues(a.criteria[i], b.criteria[i], orders[i]);
			if (comparedResult !== 0) return comparedResult;
		}
		return 0;
	}).map((item) => item.original);
}
//#endregion
exports.orderBy = orderBy;
