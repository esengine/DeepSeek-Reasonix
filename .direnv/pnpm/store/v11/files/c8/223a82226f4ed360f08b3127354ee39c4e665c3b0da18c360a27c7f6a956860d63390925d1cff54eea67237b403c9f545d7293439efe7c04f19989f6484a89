"use strict";
Object.defineProperty(exports, "__esModule", { value: true });
exports.default = isSpecialUse;
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
//# sourceMappingURL=is-special-use.js.map