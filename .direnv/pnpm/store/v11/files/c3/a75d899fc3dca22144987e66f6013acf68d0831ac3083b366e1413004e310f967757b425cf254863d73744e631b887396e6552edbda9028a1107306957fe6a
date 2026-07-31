const require_debounce = require("../../function/debounce.js");
//#region src/compat/function/debounce.ts
function debounce(func, debounceMs = 0, options = {}) {
	if (typeof options !== "object") options = {};
	const { leading = false, trailing = true, maxWait } = options;
	const edges = Array(2);
	if (leading) edges[0] = "leading";
	if (trailing) edges[1] = "trailing";
	let result = void 0;
	let pendingAt = null;
	const _debounced = require_debounce.debounce(function(...args) {
		result = func.apply(this, args);
		pendingAt = null;
	}, debounceMs, { edges });
	const debounced = function(...args) {
		if (maxWait != null) {
			if (pendingAt === null) pendingAt = Date.now();
			if (Date.now() - pendingAt >= maxWait) {
				result = func.apply(this, args);
				pendingAt = Date.now();
				_debounced.cancel();
				_debounced.schedule();
				return result;
			}
		}
		_debounced.apply(this, args);
		return result;
	};
	const flush = () => {
		_debounced.flush();
		return result;
	};
	debounced.cancel = _debounced.cancel;
	debounced.flush = flush;
	return debounced;
}
//#endregion
exports.debounce = debounce;
