"use strict";
/**
 * Implement a factory allowing to plug different implementations of suffix
 * lookup (e.g.: using a trie or the packed hashes datastructures). This is used
 * and exposed in `tldts.ts` and `tldts-experimental.ts` bundle entrypoints.
 */
Object.defineProperty(exports, "__esModule", { value: true });
exports.getEmptyResult = getEmptyResult;
exports.resetResult = resetResult;
exports.parseImpl = parseImpl;
const domain_1 = require("./domain");
const domain_without_suffix_1 = require("./domain-without-suffix");
const extract_hostname_1 = require("./extract-hostname");
const is_ip_1 = require("./is-ip");
const is_special_use_1 = require("./is-special-use");
const is_valid_1 = require("./is-valid");
const options_1 = require("./options");
const subdomain_1 = require("./subdomain");
function getEmptyResult() {
    return {
        domain: null,
        domainWithoutSuffix: null,
        hostname: null,
        isIcann: null,
        isIp: null,
        isPrivate: null,
        isSpecialUse: null,
        publicSuffix: null,
        subdomain: null,
    };
}
function resetResult(result) {
    result.domain = null;
    result.domainWithoutSuffix = null;
    result.hostname = null;
    result.isIcann = null;
    result.isIp = null;
    result.isPrivate = null;
    result.isSpecialUse = null;
    result.publicSuffix = null;
    result.subdomain = null;
}
function parseImpl(url, step, suffixLookup, partialOptions, result) {
    const options = /*@__INLINE__*/ (0, options_1.setDefaults)(partialOptions);
    // Very fast approximate check to make sure `url` is a string. This is needed
    // because the library will not necessarily be used in a typed setup and
    // values of arbitrary types might be given as argument.
    if (typeof url !== 'string') {
        return result;
    }
    // Extract hostname from `url` only if needed. This can be made optional
    // using `options.extractHostname`. This option will typically be used
    // whenever we are sure the inputs to `parse` are already hostnames and not
    // arbitrary URLs.
    //
    // `mixedInput` allows to specify if we expect a mix of URLs and hostnames
    // as input. If only hostnames are expected then `extractHostname` can be
    // set to `false` to speed-up parsing. If only URLs are expected then
    // `mixedInputs` can be set to `false`. The `mixedInputs` is only a hint
    // and will not change the behavior of the library.
    // Whether `url` itself was already a valid hostname (only computed on the
    // mixedInputs path). Lets us skip the post-extraction validation below when
    // extractHostname returned `url` unchanged (same reference).
    let urlIsValid = false;
    if (!options.extractHostname) {
        result.hostname = url;
    }
    else if (options.mixedInputs) {
        urlIsValid = (0, is_valid_1.default)(url);
        result.hostname = (0, extract_hostname_1.default)(url, urlIsValid, options.validateHostname);
    }
    else {
        result.hostname = (0, extract_hostname_1.default)(url, false, options.validateHostname);
    }
    // Check if `hostname` is a valid ip address
    if (options.detectIp && result.hostname !== null) {
        result.isIp = (0, is_ip_1.default)(result.hostname);
        if (result.isIp) {
            return result;
        }
    }
    // Perform hostname validation if enabled. If hostname is not valid, no need to
    // go further as there will be no valid domain or sub-domain. This validation
    // is applied before any early returns to ensure consistent behavior across
    // all API methods including getHostname().
    if (options.validateHostname &&
        options.extractHostname &&
        result.hostname !== null &&
        // Skip the re-scan when `url` was already validated and extractHostname
        // returned it unchanged (same reference => identical string, still valid).
        !(urlIsValid && result.hostname === url) &&
        // Skip the re-scan when extractHostname already validated the host inline
        // (a confirmed-valid simple authority — see extract-hostname.ts).
        !extract_hostname_1.extractedHostnameValidated &&
        !(0, is_valid_1.default)(result.hostname)) {
        result.hostname = null;
        return result;
    }
    if (step === 0 /* FLAG.HOSTNAME */ || result.hostname === null) {
        return result;
    }
    // Flag special-use domains, only when opted in (`detectSpecialUse`) and only
    // for the full `parse()` result (FLAG.ALL). Computed here, before the
    // public-suffix/domain early-returns below, so single-label names like
    // `localhost` (which have no registrable domain) are still flagged.
    if (step === 5 /* FLAG.ALL */ && options.detectSpecialUse) {
        result.isSpecialUse = (0, is_special_use_1.default)(result.hostname);
    }
    // Extract public suffix
    suffixLookup(result.hostname, options, result);
    if (step === 2 /* FLAG.PUBLIC_SUFFIX */ || result.publicSuffix === null) {
        return result;
    }
    // Extract domain
    result.domain = (0, domain_1.default)(result.publicSuffix, result.hostname, options);
    if (step === 3 /* FLAG.DOMAIN */ || result.domain === null) {
        return result;
    }
    // Extract subdomain
    result.subdomain = (0, subdomain_1.default)(result.hostname, result.domain);
    if (step === 4 /* FLAG.SUB_DOMAIN */) {
        return result;
    }
    // Extract domain without suffix
    result.domainWithoutSuffix = (0, domain_without_suffix_1.default)(result.domain, result.publicSuffix);
    return result;
}
//# sourceMappingURL=factory.js.map