/**
* Get viewBox from string
*/
function getSVGViewBox(value) {
	const result = value.trim().split(/\s+/).map(Number);
	if (result.length === 4 && result.reduce((prev, value) => prev && !isNaN(value), true)) return result;
}
export { getSVGViewBox };
