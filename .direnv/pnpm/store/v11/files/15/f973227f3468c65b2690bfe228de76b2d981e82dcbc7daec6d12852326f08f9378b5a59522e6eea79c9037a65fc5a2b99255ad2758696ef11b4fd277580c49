'use strict';

/**
 * Check if `vhost` is a valid suffix of `hostname` (top-domain)
 *
 * It means that `vhost` needs to be a suffix of `hostname` and we then need to
 * make sure that: either they are equal, or the character preceding `vhost` in
 * `hostname` is a '.' (it should not be a partial label).
 *
 * * hostname = 'not.evil.com' and vhost = 'vil.com'      => not ok
 * * hostname = 'not.evil.com' and vhost = 'evil.com'     => ok
 * * hostname = 'not.evil.com' and vhost = 'not.evil.com' => ok
 */
function shareSameDomainSuffix(hostname, vhost) {
    if (hostname.endsWith(vhost)) {
        return (hostname.length === vhost.length ||
            hostname[hostname.length - vhost.length - 1] === '.');
    }
    return false;
}
/**
 * Given a hostname and its public suffix, extract the general domain.
 */
function extractDomainWithSuffix(hostname, publicSuffix) {
    // Locate the index of the last '.' in the part of the `hostname` preceding
    // the public suffix.
    //
    // examples:
    //   1. not.evil.co.uk  => evil.co.uk
    //         ^    ^
    //         |    | start of public suffix
    //         | index of the last dot
    //
    //   2. example.co.uk   => example.co.uk
    //     ^       ^
    //     |       | start of public suffix
    //     |
    //     | (-1) no dot found before the public suffix
    const publicSuffixIndex = hostname.length - publicSuffix.length - 2;
    const lastDotBeforeSuffixIndex = hostname.lastIndexOf('.', publicSuffixIndex);
    // No '.' found, then `hostname` is the general domain (no sub-domain)
    if (lastDotBeforeSuffixIndex === -1) {
        return hostname;
    }
    // Extract the part between the last '.'
    return hostname.slice(lastDotBeforeSuffixIndex + 1);
}
/**
 * Detects the domain based on rules and upon and a host string
 */
function getDomain(suffix, hostname, options) {
    // Check if `hostname` ends with a member of `validHosts`.
    if (options.validHosts !== null) {
        const validHosts = options.validHosts;
        for (const vhost of validHosts) {
            if ( /*@__INLINE__*/shareSameDomainSuffix(hostname, vhost)) {
                return vhost;
            }
        }
    }
    let numberOfLeadingDots = 0;
    if (hostname.startsWith('.')) {
        while (numberOfLeadingDots < hostname.length &&
            hostname[numberOfLeadingDots] === '.') {
            numberOfLeadingDots += 1;
        }
    }
    // If `hostname` is a valid public suffix, then there is no domain to return.
    // Since we already know that `getPublicSuffix` returns a suffix of `hostname`
    // there is no need to perform a string comparison and we only compare the
    // size.
    if (suffix.length === hostname.length - numberOfLeadingDots) {
        return null;
    }
    // To extract the general domain, we start by identifying the public suffix
    // (if any), then consider the domain to be the public suffix with one added
    // level of depth. (e.g.: if hostname is `not.evil.co.uk` and public suffix:
    // `co.uk`, then we take one more level: `evil`, giving the final result:
    // `evil.co.uk`).
    return /*@__INLINE__*/ extractDomainWithSuffix(hostname, suffix);
}

/**
 * Return the part of domain without suffix.
 *
 * Example: for domain 'foo.com', the result would be 'foo'.
 */
function getDomainWithoutSuffix(domain, suffix) {
    // Note: here `domain` and `suffix` cannot have the same length because in
    // this case we set `domain` to `null` instead. It is thus safe to assume
    // that `suffix` is shorter than `domain`.
    return domain.slice(0, -suffix.length - 1);
}

/**
 * Matches an ASCII tab (U+0009) or newline (U+000A / U+000D). The WHATWG URL
 * parser strips these before parsing; we only allocate a cleaned copy (and
 * re-parse) on the rare input that actually contains one.
 */
const CONTROL_CHARS = /[\t\n\r]/g;
// Set by `extractHostname` (a module-scope flag, read synchronously by
// `parseImpl` right after the call — same pattern as the reused RESULT object).
// `true` ONLY when extraction validated the returned host inline (a confirmed-
// valid, "simple" authority) so `parseImpl` can skip the separate
// `isValidHostname` pass. `false` in every other case (validation disabled, a
// complex authority — userinfo/port/brackets/trailing-dot/control — an invalid
// host, or a non-main return path); `parseImpl` then validates as usual. The
// fast path can only ever SKIP a redundant scan for hosts already known valid,
// never accept an invalid one.
let extractedHostnameValidated = false;
/**
 * True if char `code` is a valid hostname character. This is the per-char half
 * of `is-valid.ts`'s `isValidAscii` (a-z, 0-9, > U+007F) PLUS three additions:
 * A-Z (the host is lowercased before validation, so uppercase ≡ a valid
 * lowercase letter) and '-' / '_' (valid inside a label). KEEP IN SYNC with
 * `is-valid.ts`: these rules are deliberately duplicated to validate during
 * extraction, so any change to the accepted character set there must be
 * mirrored here (and vice-versa).
 */
function isValidHostnameChar(code) {
    return ((code >= 97 && code <= 122) || // a-z
        (code >= 48 && code <= 57) || // 0-9
        code > 127 || // non-ASCII (accepted, not punycode-checked)
        (code >= 65 && code <= 90) || // A-Z (becomes valid once lowercased)
        code === 45 || // '-'
        code === 95 // '_'
    );
}
/**
 * Classify scheme `url.slice(schemeStart, colonIndex)` as a WHATWG special
 * scheme without allocating a substring (case-insensitive via `| 32`).
 * Special schemes: ftp, file, http, https, ws, wss
 * (https://url.spec.whatwg.org/#special-scheme).
 *
 * @returns 0 = not special, 1 = special, 2 = file (its host sits only between
 *          "//" and the next slash).
 */
function getSpecialScheme(url, schemeStart, colonIndex) {
    const length = colonIndex - schemeStart;
    const c0 = url.charCodeAt(schemeStart) | 32;
    if (length === 2) {
        return c0 === 119 && (url.charCodeAt(schemeStart + 1) | 32) === 115 ? 1 : 0; // ws
    }
    else if (length === 3) {
        const c1 = url.charCodeAt(schemeStart + 1) | 32;
        const c2 = url.charCodeAt(schemeStart + 2) | 32;
        if (c0 === 119 && c1 === 115 && c2 === 115)
            return 1; // wss
        if (c0 === 102 && c1 === 116 && c2 === 112)
            return 1; // ftp
        return 0;
    }
    else if (length === 4) {
        const c1 = url.charCodeAt(schemeStart + 1) | 32;
        const c2 = url.charCodeAt(schemeStart + 2) | 32;
        const c3 = url.charCodeAt(schemeStart + 3) | 32;
        if (c0 === 104 && c1 === 116 && c2 === 116 && c3 === 112)
            return 1; // http
        if (c0 === 102 && c1 === 105 && c2 === 108 && c3 === 101)
            return 2; // file
        return 0;
    }
    else if (length === 5) {
        return c0 === 104 &&
            (url.charCodeAt(schemeStart + 1) | 32) === 116 &&
            (url.charCodeAt(schemeStart + 2) | 32) === 116 &&
            (url.charCodeAt(schemeStart + 3) | 32) === 112 &&
            (url.charCodeAt(schemeStart + 4) | 32) === 115
            ? 1
            : 0; // https
    }
    return 0;
}
/**
 * Extract a hostname from `url`, matching a WHATWG URL parser's host-boundary
 * behaviour (https://url.spec.whatwg.org/#concept-basic-url-parser) for tldts'
 * scope. It deliberately does NOT normalise the host (no IDNA/punycode or IPv4
 * canonicalisation; IPv6 brackets are stripped, not compressed), strips trailing
 * dots, and stays lenient where a strict parser rejects (bare host:port,
 * out-of-range port, user@host) — all documented deviations.
 *
 * @param urlIsValidHostname - when true, `url` is already a valid hostname and is
 *   returned by the same reference (factory.ts skips re-validation on that
 *   identity), keeping the common path allocation-free.
 * @param validate - when true, validate the host inline during the authority
 *   scan and publish the verdict via `extractedHostnameValidated` so `parseImpl`
 *   can skip the redundant `isValidHostname` pass for simple authorities.
 */
function extractHostname(url, urlIsValidHostname, validate = false) {
    let start = 0;
    let end = url.length;
    let hasUpper = false;
    let isSpecial = false;
    extractedHostnameValidated = false;
    if (!urlIsValidHostname) {
        // Data URLs never carry a host (and may be huge — short-circuit them).
        if (url.startsWith('data:')) {
            return null;
        }
        // WHATWG step 1: trim leading/trailing C0 control or space (<= U+0020).
        // Tab/newline elsewhere are handled lazily below.
        while (start < url.length && url.charCodeAt(start) <= 32) {
            start += 1;
        }
        while (end > start + 1 && url.charCodeAt(end - 1) <= 32) {
            end -= 1;
        }
        if (url.charCodeAt(start) === 47 /* '/' */ &&
            url.charCodeAt(start + 1) === 47 /* '/' */) {
            // Scheme-relative reference ("//host/path").
            start += 2;
        }
        else {
            const indexOfProtocol = url.indexOf(':/', start);
            if (indexOfProtocol !== -1) {
                // "scheme://…". Classify the scheme, then position `start` at the host.
                const special = getSpecialScheme(url, start, indexOfProtocol);
                if (special === 1) {
                    // Special scheme: skip the run of '/' and '\' after it
                    // (special-authority-(ignore-)slashes states; '\' acts as '/').
                    isSpecial = true;
                    start = indexOfProtocol + 2;
                    while (url.charCodeAt(start) === 47 /* '/' */ ||
                        url.charCodeAt(start) === 92 /* '\' */) {
                        start += 1;
                    }
                }
                else if (special === 2) {
                    // file: the host is only what sits between "//" and the next slash, so
                    // "file://h/x" => "h" but "file:///x" / "file:/x" => no host.
                    isSpecial = true;
                    start = indexOfProtocol + 1;
                    let slashes = 0;
                    while ((url.charCodeAt(start) === 47 || url.charCodeAt(start) === 92) &&
                        slashes < 2) {
                        start += 1;
                        slashes += 1;
                    }
                    if (slashes < 2) {
                        return null;
                    }
                }
                else {
                    // Unknown scheme: validate the WHATWG scheme grammar [A-Za-z0-9+.-];
                    // a control char means it was split by a tab/newline (strip + re-parse).
                    for (let i = start; i < indexOfProtocol; i += 1) {
                        const code = url.charCodeAt(i) | 32;
                        if (!(((code >= 97 && code <= 122) || // [a, z]
                            (code >= 48 && code <= 57) || // [0, 9]
                            code === 46 || // '.'
                            code === 45 || // '-'
                            code === 43) // '+'
                        )) {
                            const raw = url.charCodeAt(i);
                            if (raw === 9 || raw === 10 || raw === 13) {
                                return extractHostname(url.replace(CONTROL_CHARS, ''), urlIsValidHostname, validate);
                            }
                            return null;
                        }
                    }
                    // A non-special scheme has an authority only after "//" (else it is an
                    // opaque path with no host). `indexOf(':/')` already gave the first '/'.
                    if (url.charCodeAt(indexOfProtocol + 2) === 47 /* '/' */) {
                        start = indexOfProtocol + 3;
                    }
                    else {
                        return null;
                    }
                }
            }
            else if (url.charCodeAt(start) !== 91 /* '[' */) {
                // Cold path: no scheme "://", and not a bare IPv6 literal (whose first
                // ':' would otherwise look like a scheme separator; "[…]" falls through
                // to the ipv6 handling below). May be a bare host, a host:port, a
                // user@host, a slash-less special scheme ("https:host"), or an opaque
                // URI ("mailto:", "tel:", "urn:…").
                let indexOfColon = -1;
                for (let i = start; i < end; i += 1) {
                    const code = url.charCodeAt(i);
                    if (code === 9 || code === 10 || code === 13) {
                        return extractHostname(url.replace(CONTROL_CHARS, ''), urlIsValidHostname, validate);
                    }
                    if (code === 58 /* ':' */) {
                        indexOfColon = i;
                        break;
                    }
                    if (code === 47 || code === 92 || code === 63 || code === 35) {
                        break;
                    }
                }
                if (indexOfColon !== -1) {
                    // An '@' before the next delimiter => the ':' is userinfo, not a
                    // scheme ("user:pass@host", "mailto:a@b"): keep the whole authority.
                    let hasIdentifier = false;
                    for (let i = indexOfColon + 1; i < end; i += 1) {
                        const code = url.charCodeAt(i);
                        if (code === 47 || code === 92 || code === 63 || code === 35) {
                            break;
                        }
                        if (code === 64 /* '@' */) {
                            hasIdentifier = true;
                            break;
                        }
                    }
                    if (!hasIdentifier) {
                        // All-digits after ':' => a bare "host:port" (tldts accepts
                        // hostnames too); keep `start` and let the port handling trim it.
                        let allDigits = true;
                        let i = indexOfColon + 1;
                        for (; i < end; i += 1) {
                            const code = url.charCodeAt(i);
                            if (code === 47 || code === 92 || code === 63 || code === 35) {
                                break;
                            }
                            if (code < 48 /* '0' */ || code > 57 /* '9' */) {
                                allDigits = false;
                                break;
                            }
                        }
                        if (i === indexOfColon + 1) {
                            allDigits = false; // nothing after ':' => not a port
                        }
                        if (!allDigits) {
                            const special = getSpecialScheme(url, start, indexOfColon);
                            if (special === 0) {
                                // No "://" anywhere on the cold path and not a special scheme.
                                // A second ':' before the host's end marks a bare, unbracketed
                                // IPv6 literal ("2a01:e35::1"): fall through and let the host
                                // loop + isIp classify it. Without one this is an opaque path
                                // with no host ("mailto:x", "foo:bar").
                                let isBareIpv6 = false;
                                for (let j = indexOfColon + 1; j < end; j += 1) {
                                    const code = url.charCodeAt(j);
                                    if (code === 47 ||
                                        code === 92 ||
                                        code === 63 ||
                                        code === 35) {
                                        break;
                                    }
                                    if (code === 58 /* ':' */) {
                                        isBareIpv6 = true;
                                        break;
                                    }
                                }
                                if (!isBareIpv6) {
                                    return null;
                                }
                            }
                            else {
                                isSpecial = true;
                                start = indexOfColon + 1;
                                if (special === 2) {
                                    // file (e.g. "file:\\host"): host only between "//" and next slash.
                                    let slashes = 0;
                                    while ((url.charCodeAt(start) === 47 ||
                                        url.charCodeAt(start) === 92) &&
                                        slashes < 2) {
                                        start += 1;
                                        slashes += 1;
                                    }
                                    if (slashes < 2) {
                                        return null;
                                    }
                                }
                                else {
                                    while (url.charCodeAt(start) === 47 ||
                                        url.charCodeAt(start) === 92) {
                                        start += 1;
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
        // Find the host's end: first '/', '?' or '#' (and '\' for special URLs,
        // which WHATWG treats like '/'). Track the last '@', ']' and ':' for
        // userinfo, ipv6 and port, plus the first ':' of the host (reset at each
        // '@') to tell a bare IPv6 (>= 2 colons) from a host:port (exactly one);
        // flag uppercase and a stray tab/newline. The loop is split on `code < 64`
        // so common host characters take fewer comparisons.
        //
        // When `validate`, also accumulate `is-valid.ts`'s checks over the scanned
        // run so a simple authority's host can be validated in this single pass.
        // `vValid` only stays meaningful for a "simple" authority (no userinfo, port,
        // brackets, control or trailing dot); those cases clear it / are rejected by
        // the guard below, falling back to `isValidHostname`.
        let indexOfIdentifier = -1;
        let indexOfClosingBracket = -1;
        let indexOfPort = -1;
        let indexOfFirstColon = -1;
        let hasControl = false;
        let vValid = validate; // seeded true when validating; cleared on the first invalid char
        let vLastDot = start - 1; // mirrors is-valid.ts `lastDotIndex = -1` at host start
        let vLastCode = -1;
        if (validate && start < end) {
            // First-char rule: must be a valid host char, '.', or '_' (NOT '-').
            const c0 = url.charCodeAt(start);
            if (!(
            /*@__INLINE__*/ (isValidHostnameChar(c0) ||
                c0 === 46 /* '.' */ ||
                c0 === 95 /* '_' */)) ||
                c0 === 45 /* '-' (isValidHostnameChar allows it mid-label, not first) */) {
                vValid = false;
            }
        }
        for (let i = start; i < end; i += 1) {
            const code = url.charCodeAt(i);
            if (code < 64) {
                if (code === 47 || code === 35 || code === 63) {
                    end = i;
                    break;
                }
                else if (code === 58 /* ':' */) {
                    if (indexOfFirstColon === -1) {
                        indexOfFirstColon = i;
                    }
                    indexOfPort = i;
                }
                else if (code === 9 || code === 10 || code === 13) {
                    hasControl = true;
                }
                else if (validate) {
                    if (code === 46 /* '.' */) {
                        if (i - vLastDot > 64 || vLastCode === 46 || vLastCode === 45) {
                            vValid = false;
                        }
                        vLastDot = i;
                    }
                    else if (code < 48 || code > 57) {
                        // < 64 and not a delimiter/dot/digit => only '-' (45) is a valid
                        // host char here; everything else (space, %, !, etc.) is invalid.
                        // A '-' must also not START a label (the byte right after a '.') —
                        // mirrors is-valid.ts; the first label is covered by the first-char
                        // rule above. (RFC 1034 §3.5 / RFC 1035 §2.3.1 LDH.)
                        if (code !== 45 || vLastCode === 46 /* label-leading '-' */) {
                            vValid = false;
                        }
                    }
                }
            }
            else if (isSpecial && code === 92 /* '\' */) {
                end = i;
                break;
            }
            else if (code === 64 /* '@' */) {
                indexOfIdentifier = i;
                indexOfFirstColon = -1; // colons before '@' are userinfo, not the host
            }
            else if (code === 93 /* ']' */) {
                indexOfClosingBracket = i;
            }
            else if (code >= 65 && code <= 90) {
                hasUpper = true;
            }
            else if (validate && !( /*@__INLINE__*/isValidHostnameChar(code))) {
                // >= 64, not '@'/']'/upper: valid only if a-z, '_', or non-ASCII.
                vValid = false;
            }
            if (validate) {
                vLastCode = code;
            }
        }
        // A tab/newline inside the authority: strip everything and re-parse (rare).
        if (hasControl) {
            return extractHostname(url.replace(CONTROL_CHARS, ''), urlIsValidHostname, validate);
        }
        // Skip userinfo. '>= start' so an empty userinfo ("http://@host") works too.
        if (indexOfIdentifier !== -1 &&
            indexOfIdentifier >= start &&
            indexOfIdentifier < end) {
            start = indexOfIdentifier + 1;
        }
        if (url.charCodeAt(start) === 91 /* '[' */) {
            // ipv6 address: return what is between the brackets, or null if unclosed.
            if (indexOfClosingBracket !== -1) {
                return url.slice(start + 1, indexOfClosingBracket).toLowerCase();
            }
            return null;
        }
        else if (indexOfPort !== -1 &&
            indexOfPort > start &&
            indexOfPort < end &&
            // A host:port has exactly one ':' in the host (so its first ':' is its
            // last); a bare, unbracketed IPv6 literal ("2a01:e35::1") has >= 2, so
            // its first ':' precedes the last. Only the former has a ':port' to trim.
            indexOfFirstColon === indexOfPort) {
            end = indexOfPort; // trim ':port'
        }
        // Empty authority ("http://", "file:///path", "//"); only reachable here via
        // extraction — a bare valid hostname never lands here.
        if (start >= end) {
            return null;
        }
        // Publish the inline-validation verdict — but only for a "simple" authority,
        // where the scanned run equals the final host: no userinfo skip, no port
        // trim, no brackets, no trailing dot (trimmed below), and length within RFC
        // limits. Anything else leaves it `false` so `parseImpl` re-validates.
        //
        // Every clause below is load-bearing for CORRECTNESS, not just speed: the
        // loop accumulates `vValid` over the whole scanned run (it does not stop at
        // ':' or '@', so any port/userinfo bytes are included), so the verdict is
        // only sound when that run equals the final host. Do not drop a clause as
        // "redundant" — e.g. without `indexOfPort === -1`, `host:8080` would be
        // wrongly accepted.
        if (validate &&
            vValid &&
            indexOfIdentifier === -1 &&
            indexOfPort === -1 &&
            indexOfClosingBracket === -1 &&
            url.charCodeAt(end - 1) !== 46 /* no trailing dot */ &&
            end - start <= 255 && // total length
            end - vLastDot - 1 <= 63 && // last label length
            vLastCode !== 45 /* last char not '-' */) {
            extractedHostnameValidated = true;
        }
    }
    // Trim trailing dots
    while (end > start + 1 && url.charCodeAt(end - 1) === 46 /* '.' */) {
        end -= 1;
    }
    const hostname = start !== 0 || end !== url.length ? url.slice(start, end) : url;
    if (hasUpper) {
        return hostname.toLowerCase();
    }
    return hostname;
}

/**
 * Check if a hostname is an IP. You should be aware that this only works
 * because `hostname` is already garanteed to be a valid hostname!
 */
function isProbablyIpv4(hostname) {
    // Cannot be shorted than 1.1.1.1
    if (hostname.length < 7) {
        return false;
    }
    // Cannot be longer than: 255.255.255.255
    if (hostname.length > 15) {
        return false;
    }
    let numberOfDots = 0;
    for (let i = 0; i < hostname.length; i += 1) {
        const code = hostname.charCodeAt(i);
        if (code === 46 /* '.' */) {
            numberOfDots += 1;
        }
        else if (code < 48 /* '0' */ || code > 57 /* '9' */) {
            return false;
        }
    }
    return (numberOfDots === 3 &&
        hostname.charCodeAt(0) !== 46 /* '.' */ &&
        hostname.charCodeAt(hostname.length - 1) !== 46 /* '.' */);
}
/**
 * Similar to isProbablyIpv4.
 */
function isProbablyIpv6(hostname) {
    if (hostname.length < 3) {
        return false;
    }
    let start = hostname.startsWith('[') ? 1 : 0;
    let end = hostname.length;
    if (hostname[end - 1] === ']') {
        end -= 1;
    }
    // We only consider the maximum size of a normal IPV6. Note that this will
    // fail on so-called "IPv4 mapped IPv6 addresses" but this is a corner-case
    // and a proper validation library should be used for these.
    if (end - start > 39) {
        return false;
    }
    let hasColon = false;
    for (; start < end; start += 1) {
        const code = hostname.charCodeAt(start);
        if (code === 58 /* ':' */) {
            hasColon = true;
        }
        else if (!(((code >= 48 && code <= 57) || // 0-9
            (code >= 97 && code <= 102) || // a-f
            (code >= 65 && code <= 70)) // A-F (RFC 4291 §2.2: an IPv6 hextet is hex digits only)
        )) {
            return false;
        }
    }
    return hasColon;
}
/**
 * Check if `hostname` is *probably* a valid ip addr (either ipv6 or ipv4).
 * This *will not* work on any string. We need `hostname` to be a valid
 * hostname.
 */
function isIp(hostname) {
    return isProbablyIpv6(hostname) || isProbablyIpv4(hostname);
}

/**
 * Special-use domain names from the IANA "Special-Use Domain Names" registry:
 * the authoritative list, created by RFC 6761 and maintained as new RFCs add to
 * it: https://www.iana.org/assignments/special-use-domain-names/
 * Snapshot: 2026-05-24. (RFC 6761 is not obsoleted; draft-hoffman-rfc6761bis
 * proposes to retire its prose but keep this registry, so the registry is the
 * source of truth; re-sync this list against it.)
 *
 * These names never correspond to a public registration, yet neither
 * `isIcann` nor `isPrivate` marks one as special-use: most are absent from the
 * Public Suffix List (so `a.test` looks like a registrable domain), and the
 * few that are listed (`onion`, `home.arpa`) appear there as ordinary ICANN
 * suffixes. `isSpecialUse` is the single signal that covers them all.
 *
 * Per the registry and RFC 6761 ("and any names falling within these domains"),
 * the designation covers each listed name AND all of its sub-domains. DNS labels
 * are case-insensitive (RFC 4343); `hostname` is expected to be already
 * lower-cased and trailing-dot-stripped, as produced by `extractHostname`, the
 * same normalization the Public-Suffix-List lookup relies on.
 *
 * Two groups of registry entries are intentionally excluded: the numeric
 * reverse-DNS delegation zones (`10.in-addr.arpa`, the `*.ip6.arpa` ranges, …),
 * which are reverse-DNS PTR zones rather than hostnames and whose parents
 * (`in-addr.arpa`/`ip6.arpa`) are already in the Public Suffix List; and the
 * deprecated `eap-noob.arpa` entry.
 */
const SPECIAL_USE_DOMAINS = [
    'test', // RFC 6761
    'localhost', // RFC 6761
    'invalid', // RFC 6761
    'example', // RFC 6761
    'example.com', // RFC 6761
    'example.net', // RFC 6761
    'example.org', // RFC 6761
    'local', // RFC 6762 (mDNS)
    'onion', // RFC 7686 (Tor)
    'alt', // RFC 9476
    'home.arpa', // RFC 8375
    'ipv4only.arpa', // RFC 8880
    'resolver.arpa', // RFC 9462
    'service.arpa', // RFC 9665
    '6tisch.arpa', // RFC 9031
    'eap.arpa', // RFC 9965
];
/**
 * Return `true` if `hostname` is, or is a sub-domain of, a special-use domain
 * (see the registry note above). Expects an already-normalized `hostname`.
 */
function isSpecialUse(hostname) {
    for (const name of SPECIAL_USE_DOMAINS) {
        // Match on a label boundary: `hostname` is either exactly `name` or ends
        // with `.name` (so `latest` is not matched by `test`, nor `myexample.com`
        // by `example.com`).
        if (hostname.endsWith(name) &&
            (hostname.length === name.length ||
                hostname.charCodeAt(hostname.length - name.length - 1) === 46) /* '.' */) {
            return true;
        }
    }
    return false;
}

/**
 * Implements fast shallow verification of hostnames. This does not perform a
 * struct check on the content of labels (classes of Unicode characters, etc.)
 * but instead check that the structure is valid (number of labels, length of
 * labels, etc.).
 *
 * If you need stricter validation, consider using an external library.
 */
// KEEP IN SYNC with `extract-hostname.ts` `isValidHostnameChar` + its inline
// scan/verdict, which duplicate these structural rules to validate during
// extraction (a perf fusion). That copy additionally accepts A-Z (the host is
// not yet lowercased there) and folds in '-' / '_'. Any change to the accepted
// character set or the label/length rules here must be mirrored there.
function isValidAscii(code) {
    return ((code >= 97 && code <= 122) || (code >= 48 && code <= 57) || code > 127);
}
/**
 * Check if a hostname string is valid. It's usually a preliminary check before
 * trying to use getDomain or anything else.
 *
 * Beware: it does not check if the TLD exists.
 */
function isValidHostname (hostname) {
    if (hostname.length > 255) {
        return false;
    }
    if (hostname.length === 0) {
        return false;
    }
    if (
    /*@__INLINE__*/ !isValidAscii(hostname.charCodeAt(0)) &&
        hostname.charCodeAt(0) !== 46 && // '.' (dot)
        hostname.charCodeAt(0) !== 95 // '_' (underscore)
    ) {
        return false;
    }
    // Validate hostname according to RFC
    let lastDotIndex = -1;
    let lastCharCode = -1;
    const len = hostname.length;
    for (let i = 0; i < len; i += 1) {
        const code = hostname.charCodeAt(i);
        if (code === 46 /* '.' */) {
            if (
            // Check that previous label is < 63 bytes long (64 = 63 + '.')
            i - lastDotIndex > 64 ||
                // Check that previous character was not already a '.'
                lastCharCode === 46 ||
                // Check that the previous label does not end with '-' (RFC 1035 §2.3.1 LDH).
                // '_' is intentionally NOT restricted: DNS allows any octet (RFC 2181 §11) and
                // WHATWG URL does not treat '_' as a forbidden host code point.
                lastCharCode === 45) {
                return false;
            }
            lastDotIndex = i;
        }
        else if (
        // A forbidden character in the label...
        !( /*@__INLINE__*/(isValidAscii(code) || code === 45 || code === 95)) ||
            // ...or a '-' starting a label (the byte right after a '.'). A label must
            // not begin with a hyphen (RFC 1034 §3.5 / RFC 1035 §2.3.1 LDH, as amended
            // by RFC 1123 §2.1; cf. UTS #46 CheckHyphens). The first label is covered by
            // the leading-character guard above; mirrors the trailing-'-' rule below.
            (code === 45 && lastCharCode === 46)) {
            return false;
        }
        lastCharCode = code;
    }
    return (
    // Check that last label is shorter than 63 chars
    len - lastDotIndex - 1 <= 63 &&
        // Check that the last character is an allowed trailing label character.
        // Since we already checked that the char is a valid hostname character,
        // we only need to check that it's different from '-'.
        lastCharCode !== 45);
}

function setDefaultsImpl({ allowIcannDomains = true, allowPrivateDomains = false, detectIp = true, detectSpecialUse = false, extractHostname = true, mixedInputs = true, validHosts = null, validateHostname = true, }) {
    return {
        allowIcannDomains,
        allowPrivateDomains,
        detectIp,
        detectSpecialUse,
        extractHostname,
        mixedInputs,
        validHosts,
        validateHostname,
    };
}
const DEFAULT_OPTIONS = /*@__INLINE__*/ setDefaultsImpl({});
function setDefaults(options) {
    if (options === undefined) {
        return DEFAULT_OPTIONS;
    }
    return /*@__INLINE__*/ setDefaultsImpl(options);
}

/**
 * Returns the subdomain of a hostname string
 */
function getSubdomain(hostname, domain) {
    // If `hostname` and `domain` are the same, then there is no sub-domain
    if (domain.length === hostname.length) {
        return '';
    }
    return hostname.slice(0, -domain.length - 1);
}

/**
 * Implement a factory allowing to plug different implementations of suffix
 * lookup (e.g.: using a trie or the packed hashes datastructures). This is used
 * and exposed in `tldts.ts` and `tldts-experimental.ts` bundle entrypoints.
 */
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
    const options = /*@__INLINE__*/ setDefaults(partialOptions);
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
        urlIsValid = isValidHostname(url);
        result.hostname = extractHostname(url, urlIsValid, options.validateHostname);
    }
    else {
        result.hostname = extractHostname(url, false, options.validateHostname);
    }
    // Check if `hostname` is a valid ip address
    if (options.detectIp && result.hostname !== null) {
        result.isIp = isIp(result.hostname);
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
        !extractedHostnameValidated &&
        !isValidHostname(result.hostname)) {
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
        result.isSpecialUse = isSpecialUse(result.hostname);
    }
    // Extract public suffix
    suffixLookup(result.hostname, options, result);
    if (step === 2 /* FLAG.PUBLIC_SUFFIX */ || result.publicSuffix === null) {
        return result;
    }
    // Extract domain
    result.domain = getDomain(result.publicSuffix, result.hostname, options);
    if (step === 3 /* FLAG.DOMAIN */ || result.domain === null) {
        return result;
    }
    // Extract subdomain
    result.subdomain = getSubdomain(result.hostname, result.domain);
    if (step === 4 /* FLAG.SUB_DOMAIN */) {
        return result;
    }
    // Extract domain without suffix
    result.domainWithoutSuffix = getDomainWithoutSuffix(result.domain, result.publicSuffix);
    return result;
}

function fastPath (hostname, options, out) {
    // Fast path for very popular suffixes; this allows to by-pass lookup
    // completely as well as any extra allocation or string manipulation.
    if (!options.allowPrivateDomains && hostname.length > 3) {
        const last = hostname.length - 1;
        const c3 = hostname.charCodeAt(last);
        const c2 = hostname.charCodeAt(last - 1);
        const c1 = hostname.charCodeAt(last - 2);
        const c0 = hostname.charCodeAt(last - 3);
        if (c3 === 109 /* 'm' */ &&
            c2 === 111 /* 'o' */ &&
            c1 === 99 /* 'c' */ &&
            c0 === 46 /* '.' */) {
            out.isIcann = true;
            out.isPrivate = false;
            out.publicSuffix = 'com';
            return true;
        }
        else if (c3 === 103 /* 'g' */ &&
            c2 === 114 /* 'r' */ &&
            c1 === 111 /* 'o' */ &&
            c0 === 46 /* '.' */) {
            out.isIcann = true;
            out.isPrivate = false;
            out.publicSuffix = 'org';
            return true;
        }
        else if (c3 === 117 /* 'u' */ &&
            c2 === 100 /* 'd' */ &&
            c1 === 101 /* 'e' */ &&
            c0 === 46 /* '.' */) {
            out.isIcann = true;
            out.isPrivate = false;
            out.publicSuffix = 'edu';
            return true;
        }
        else if (c3 === 118 /* 'v' */ &&
            c2 === 111 /* 'o' */ &&
            c1 === 103 /* 'g' */ &&
            c0 === 46 /* '.' */) {
            out.isIcann = true;
            out.isPrivate = false;
            out.publicSuffix = 'gov';
            return true;
        }
        else if (c3 === 116 /* 't' */ &&
            c2 === 101 /* 'e' */ &&
            c1 === 110 /* 'n' */ &&
            c0 === 46 /* '.' */) {
            out.isIcann = true;
            out.isPrivate = false;
            out.publicSuffix = 'net';
            return true;
        }
        else if (c3 === 101 /* 'e' */ &&
            c2 === 100 /* 'd' */ &&
            c1 === 46 /* '.' */) {
            out.isIcann = true;
            out.isPrivate = false;
            out.publicSuffix = 'de';
            return true;
        }
    }
    return false;
}

exports.fastPathLookup = fastPath;
exports.getEmptyResult = getEmptyResult;
exports.parseImpl = parseImpl;
exports.resetResult = resetResult;
exports.setDefaults = setDefaults;
//# sourceMappingURL=index.js.map
