//#region src/array/cartesianProduct.ts
function cartesianProduct(...arrs) {
	if (arrs.length === 0) return [[]];
	let total = 1;
	for (let i = 0; i < arrs.length; i++) total *= arrs[i].length;
	if (total === 0) return [];
	const n = arrs.length;
	const result = Array(total);
	for (let i = 0; i < total; i++) {
		const tuple = Array(n);
		let idx = i;
		for (let j = n - 1; j >= 0; j--) {
			const arr = arrs[j];
			const len = arr.length;
			tuple[j] = arr[idx % len];
			idx = Math.floor(idx / len);
		}
		result[i] = tuple;
	}
	return result;
}
//#endregion
exports.cartesianProduct = cartesianProduct;
