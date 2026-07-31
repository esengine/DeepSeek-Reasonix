const require_delay = require("../promise/delay.js");
//#region src/function/retry.ts
const DEFAULT_DELAY = 0;
const DEFAULT_RETRIES = Number.POSITIVE_INFINITY;
const DEFAULT_SHOULD_RETRY = () => true;
/**
* Retries a function that returns a promise with specified options.
*
* @template T
* @param func - The function to retry. It should return a promise.
* @param [_options] - Either the number of retries or an options object.
* @returns A promise that resolves with the value of the successful function call.
*/
async function retry(func, _options) {
	let delay$1;
	let retries;
	let signal;
	let shouldRetry;
	if (typeof _options === "number") {
		delay$1 = DEFAULT_DELAY;
		retries = _options;
		signal = void 0;
		shouldRetry = DEFAULT_SHOULD_RETRY;
	} else {
		delay$1 = _options?.delay ?? DEFAULT_DELAY;
		retries = _options?.retries ?? DEFAULT_RETRIES;
		signal = _options?.signal;
		shouldRetry = _options?.shouldRetry ?? DEFAULT_SHOULD_RETRY;
	}
	let error;
	for (let attempts = 0; attempts <= retries; attempts++) {
		if (signal?.aborted) throw error ?? /* @__PURE__ */ new Error(`The retry operation was aborted due to an abort signal.`);
		try {
			return await func();
		} catch (err) {
			error = err;
			if (!shouldRetry(err, attempts)) throw err;
			const currentDelay = typeof delay$1 === "function" ? delay$1(attempts) : delay$1;
			await require_delay.delay(currentDelay);
		}
	}
	throw error;
}
//#endregion
exports.retry = retry;
