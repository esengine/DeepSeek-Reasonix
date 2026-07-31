export declare let extractedHostnameValidated: boolean;
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
export default function extractHostname(url: string, urlIsValidHostname: boolean, validate?: boolean): string | null;
